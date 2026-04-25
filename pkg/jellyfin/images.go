package jellyfin

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/logocache"
)

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	imageType := chi.URLParam(r, "imageType")
	ctx := r.Context()

	isBackdrop := strings.EqualFold(imageType, "Backdrop")

	if strings.HasPrefix(itemID, "person_") {
		s.servePersonImage(w, r)
		return
	}

	if strings.HasPrefix(itemID, "group_") {
		s.serveGroupImage(w, r, addDashes(strings.TrimPrefix(itemID, "group_")))
		return
	}

	if isGroupItemID(itemID) {
		s.serveGroupImage(w, r, groupUUIDFromItemID(itemID))
		return
	}

	if strings.HasPrefix(itemID, "cccc") || isSeasonItemID(itemID) {
		seriesID := itemID
		if isSeasonItemID(itemID) {
			h, _, ok := parseSeasonItemID(itemID)
			if ok {
				seriesID = fmt.Sprintf("cccc%028x", h)
			}
		}
		s.serveSeriesImage(w, r, seriesID, isBackdrop)
		return
	}

	if stream, err := s.streams.GetByID(ctx, addDashes(itemID)); err == nil && stream != nil {
		lookupName := stream.Name
		mediaType := stream.VODType
		if stream.VODType == "series" && stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}

		if stream.VODType == "series" && stream.VODSeason > 0 && stream.VODEpisode > 0 {
			if ep := s.tmdbClient.LookupEpisode(lookupName, stream.VODSeason, stream.VODEpisode); ep != nil && ep.StillPath != "" {
				s.serveTMDBImage(w, r, "w300", ep.StillPath)
				return
			}
		}

		if isBackdrop {
			if bd := s.tmdbClient.LookupBackdrop(lookupName, mediaType); bd != "" {
				s.serveTMDBImage(w, r, "w1280", bd)
				return
			}
		}

		if posterURL := s.tmdbClient.LookupPoster(lookupName, mediaType); posterURL != "" {
			r.URL, _ = url.Parse(posterURL)
			s.tmdbClient.ServeImage(w, r)
			return
		}

		if s.logoService != nil && stream.Logo != "" {
			s.redirectToLogo(w, r, stream.Logo)
			return
		}
	}

	if channel, err := s.channels.GetByID(ctx, addDashes(itemID)); err == nil && channel != nil && s.logoService != nil {
		resolved := s.logoService.ResolveChannel(*channel)
		if resolved != "" && resolved != logocache.Placeholder && !strings.HasPrefix(resolved, "data:") {
			http.Redirect(w, r, s.mainServerURLFromRequest(r)+resolved, http.StatusTemporaryRedirect)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) serveGroupImage(w http.ResponseWriter, r *http.Request, groupID string) {
	group, err := s.channelGroups.GetByID(r.Context(), groupID)
	if err == nil && group != nil && group.ImageURL != "" && s.logoService != nil {
		resolved := s.logoService.Resolve(group.ImageURL)
		if resolved != "" && resolved != logocache.Placeholder && !strings.HasPrefix(resolved, "data:") {
			http.Redirect(w, r, s.mainServerURLFromRequest(r)+resolved, http.StatusTemporaryRedirect)
			return
		}
	}

	if s.logoService == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	channels, _ := s.channels.List(r.Context())
	for _, ch := range channels {
		if ch.ChannelGroupID != nil && *ch.ChannelGroupID == groupID {
			resolved := s.logoService.ResolveChannel(ch)
			if resolved != "" && resolved != logocache.Placeholder && !strings.HasPrefix(resolved, "data:") {
				http.Redirect(w, r, s.mainServerURLFromRequest(r)+resolved, http.StatusTemporaryRedirect)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) serveSeriesImage(w http.ResponseWriter, r *http.Request, seriesID string, isBackdrop bool) {
	streams, _ := s.streams.ListByVODType(r.Context(), "series")
	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := seriesKey(&st)
		if seriesIDFromName(key) != seriesID {
			continue
		}
		if isBackdrop {
			if bd := s.tmdbClient.LookupBackdrop(key, "series"); bd != "" {
				s.serveTMDBImage(w, r, "w1280", bd)
				return
			}
		}
		if poster := s.tmdbClient.LookupPoster(key, "tv"); poster != "" {
			r.URL, _ = url.Parse(poster)
			s.tmdbClient.ServeImage(w, r)
			return
		}
		break
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) serveTMDBImage(w http.ResponseWriter, r *http.Request, size, path string) {
	r.URL, _ = url.Parse(fmt.Sprintf("/api/tmdb/image?size=%s&path=%s", size, url.QueryEscape(path)))
	s.tmdbClient.ServeImage(w, r)
}

func (s *Server) redirectToLogo(w http.ResponseWriter, r *http.Request, logoURL string) {
	http.Redirect(w, r, s.mainServerURLFromRequest(r)+s.logoService.Resolve(logoURL), http.StatusTemporaryRedirect)
}

func (s *Server) servePersonImage(w http.ResponseWriter, r *http.Request) {
	personID := chi.URLParam(r, "personId")
	if personID == "" {
		personID = chi.URLParam(r, "itemId")
	}
	tmdbID := strings.TrimPrefix(personID, "person_")
	if tmdbID == "" || tmdbID == personID {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	details, err := s.tmdbClient.Details("person", tmdbID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	dm, ok := details.(map[string]any)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	profilePath, _ := dm["profile_path"].(string)
	if profilePath == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	s.serveTMDBImage(w, r, "w185", profilePath)
}

func (s *Server) mainServerURL() string {
	return s.baseURL + ":8080"
}

func (s *Server) mainServerURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	if scheme == "https" || strings.Contains(host, ":") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":8080"
}
