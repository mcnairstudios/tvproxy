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

	if strings.HasPrefix(itemID, "group_") {
		s.serveGroupImage(w, r, addDashes(strings.TrimPrefix(itemID, "group_")))
		return
	}

	if strings.HasPrefix(itemID, "series_") {
		s.serveSeriesImage(w, r, itemID, isBackdrop)
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
			http.Redirect(w, r, s.mainServerURL()+resolved, http.StatusTemporaryRedirect)
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
			http.Redirect(w, r, s.mainServerURL()+resolved, http.StatusTemporaryRedirect)
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
				http.Redirect(w, r, s.mainServerURL()+resolved, http.StatusTemporaryRedirect)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) serveSeriesImage(w http.ResponseWriter, r *http.Request, seriesID string, isBackdrop bool) {
	streams, _ := s.streams.List(r.Context())
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
	http.Redirect(w, r, s.mainServerURL()+s.logoService.Resolve(logoURL), http.StatusTemporaryRedirect)
}

func (s *Server) mainServerURL() string {
	return s.baseURL + ":8080"
}
