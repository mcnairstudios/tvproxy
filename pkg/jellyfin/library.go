package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

const (
	viewMoviesID  = "f0000000-0000-0000-0000-000000000001"
	viewTVID      = "f0000000-0000-0000-0000-000000000002"
	viewLiveTVID  = "f0000000-0000-0000-0000-000000000004"
)

func (s *Server) userViews(w http.ResponseWriter, r *http.Request) {
	views := []BaseItemDto{
		{
			Name:           "Movies",
			ServerID:       s.serverID,
			ID:             viewMoviesID,
			Type:           "CollectionFolder",
			CollectionType: "movies",
			IsFolder:       true,
			ImageTags:      map[string]string{},
		},
		{
			Name:           "TV Shows",
			ServerID:       s.serverID,
			ID:             viewTVID,
			Type:           "CollectionFolder",
			CollectionType: "tvshows",
			IsFolder:       true,
			ImageTags:      map[string]string{},
		},
		{
			Name:           "Live TV",
			ServerID:       s.serverID,
			ID:             viewLiveTVID,
			Type:           "CollectionFolder",
			CollectionType: "livetv",
			IsFolder:       true,
			ImageTags:      map[string]string{},
		},
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            views,
		TotalRecordCount: len(views),
	})
}

func (s *Server) getItems(w http.ResponseWriter, r *http.Request) {
	parentID := r.URL.Query().Get("parentId")
	if parentID == "" {
		parentID = r.URL.Query().Get("ParentId")
	}
	itemTypes := r.URL.Query().Get("includeItemTypes")
	if itemTypes == "" {
		itemTypes = r.URL.Query().Get("IncludeItemTypes")
	}

	ctx := r.Context()

	switch {
	case parentID == viewMoviesID || strings.Contains(itemTypes, "Movie"):
		s.getMovies(w, r, ctx)
	case parentID == viewTVID || strings.Contains(itemTypes, "Series"):
		s.getSeries(w, r, ctx)
	case parentID == viewLiveTVID:
		s.liveTvChannels(w, r)
	default:
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
			Items:            []BaseItemDto{},
			TotalRecordCount: 0,
		})
	}
}

func (s *Server) getMovies(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	streams, err := s.streams.List(ctx)
	if err != nil {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	var items []BaseItemDto
	for _, st := range streams {
		if st.VODType != "movie" {
			continue
		}

		item := BaseItemDto{
			Name:         st.Name,
			ServerID:     s.serverID,
			ID:           strings.ReplaceAll(st.ID, "-", ""),
			Type:         "Movie",
			MediaType:    "Video",
			IsFolder:     false,
			LocationType: "FileSystem",
			ImageTags:    map[string]string{},
			UserData:     &UserItemData{Key: st.ID},
		}

		lookupName := st.Name
		if st.VODCollection != "" {
			lookupName = st.VODCollection
		}

		if m := s.tmdbClient.LookupMovie(lookupName); m != nil {
			item.Overview = m.Overview
			item.CommunityRating = m.Rating
			item.OfficialRating = m.Certification
			item.Genres = m.Genres
			if m.Year != "" {
				if yr, err := strconv.Atoi(m.Year); err == nil {
					item.ProductionYear = yr
				}
			}
			if m.PosterPath != "" {
				item.ImageTags["Primary"] = "tmdb"
			}
		}

		if st.VODDuration > 0 {
			item.RunTimeTicks = int64(st.VODDuration * 10000000)
		}

		items = append(items, item)
	}

	if items == nil {
		items = []BaseItemDto{}
	}

	startIndex := 0
	limit := len(items)
	if si := r.URL.Query().Get("startIndex"); si != "" {
		startIndex, _ = strconv.Atoi(si)
	}
	if li := r.URL.Query().Get("limit"); li != "" {
		limit, _ = strconv.Atoi(li)
	}

	total := len(items)
	if startIndex > len(items) {
		items = []BaseItemDto{}
	} else if startIndex+limit > len(items) {
		items = items[startIndex:]
	} else {
		items = items[startIndex : startIndex+limit]
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            items,
		TotalRecordCount: total,
		StartIndex:       startIndex,
	})
}

func (s *Server) getSeries(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	streams, err := s.streams.List(ctx)
	if err != nil {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	seriesMap := make(map[string]*BaseItemDto)
	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := st.VODSeries
		if key == "" {
			key = st.Name
		}
		if _, exists := seriesMap[key]; exists {
			seriesMap[key].ChildCount++
			continue
		}

		item := &BaseItemDto{
			Name:         key,
			ServerID:     s.serverID,
			ID:           fmt.Sprintf("series_%x", hashString(key)),
			Type:         "Series",
			MediaType:    "Video",
			IsFolder:     true,
			LocationType: "FileSystem",
			ChildCount:   1,
			ImageTags:    map[string]string{},
			UserData:     &UserItemData{Key: key},
		}

		if sr := s.tmdbClient.LookupSeries(key); sr != nil {
			item.Overview = sr.Overview
			item.CommunityRating = sr.Rating
			item.OfficialRating = sr.Certification
			item.Genres = sr.Genres
			if sr.Year != "" {
				if yr, err := strconv.Atoi(sr.Year); err == nil {
					item.ProductionYear = yr
				}
			}
			if sr.PosterPath != "" {
				item.ImageTags["Primary"] = "tmdb"
			}
		}

		seriesMap[key] = item
	}

	var items []BaseItemDto
	for _, item := range seriesMap {
		items = append(items, *item)
	}
	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	ctx := r.Context()

	if itemID == viewMoviesID || itemID == viewTVID || itemID == viewLiveTVID {
		s.userViews(w, r)
		return
	}

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err == nil && stream != nil {
		item := s.streamToItem(stream)

		lookupName := stream.Name
		mediaType := stream.VODType
		if stream.VODType == "series" && stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}

		if mediaType == "movie" {
			if m := s.tmdbClient.LookupMovie(lookupName); m != nil {
				item.Overview = m.Overview
				item.CommunityRating = m.Rating
				item.OfficialRating = m.Certification
				item.Genres = m.Genres
				if m.Year != "" {
					if yr, err := strconv.Atoi(m.Year); err == nil {
						item.ProductionYear = yr
					}
				}
				if m.PosterPath != "" {
					item.ImageTags = map[string]string{"Primary": "tmdb"}
					item.BackdropImageTags = []string{"tmdb"}
				}
			}
		} else if mediaType == "series" {
			if sr := s.tmdbClient.LookupSeries(lookupName); sr != nil {
				item.Overview = sr.Overview
				item.CommunityRating = sr.Rating
				item.OfficialRating = sr.Certification
				item.Genres = sr.Genres
				if sr.Year != "" {
					if yr, err := strconv.Atoi(sr.Year); err == nil {
						item.ProductionYear = yr
					}
				}
				if sr.PosterPath != "" {
					item.ImageTags = map[string]string{"Primary": "tmdb"}
					item.BackdropImageTags = []string{"tmdb"}
				}
			}
		}

		s.respondJSON(w, http.StatusOK, item)
		return
	}

	s.respondJSON(w, http.StatusOK, BaseItemDto{
		Name:     "Unknown",
		ServerID: s.serverID,
		ID:       itemID,
		Type:     "Video",
	})
}

func (s *Server) getSimilar(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (s *Server) getSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}

func (s *Server) getFilters(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Genres":          []string{},
		"Tags":            []string{},
		"OfficialRatings": []string{},
		"Years":           []int{},
	})
}

func (s *Server) getLatest(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}

func (s *Server) getResume(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (s *Server) getSeasons(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (s *Server) getEpisodes(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	ctx := r.Context()

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err == nil && stream != nil {
		lookupName := stream.Name
		mediaType := stream.VODType
		if stream.VODType == "series" && stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}

		posterURL := s.tmdbClient.LookupPoster(lookupName, mediaType)
		if posterURL != "" {
			http.Redirect(w, r, s.baseURL+":8080"+posterURL, http.StatusTemporaryRedirect)
			return
		}

		if s.logoService != nil && stream.Logo != "" {
			http.Redirect(w, r, s.baseURL+":8080"+s.logoService.Resolve(stream.Logo), http.StatusTemporaryRedirect)
			return
		}
	}

	channel, err := s.channels.GetByID(ctx, addDashes(itemID))
	if err == nil && channel != nil && channel.Logo != "" {
		http.Redirect(w, r, s.baseURL+":8080"+s.logoService.Resolve(channel.Logo), http.StatusTemporaryRedirect)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")

	s.respondJSON(w, http.StatusOK, map[string]any{
		"MediaSources": []MediaSource{
			{
				Protocol:             "Http",
				ID:                   itemID,
				Type:                 "Default",
				Name:                 "Default",
				Container:            "mp4",
				IsRemote:             true,
				SupportsTranscoding:  true,
				SupportsDirectStream: true,
				SupportsDirectPlay:   false,
				IsInfiniteStream:     false,
				TranscodingURL:       fmt.Sprintf("/Videos/%s/stream.mp4?static=true", itemID),
				TranscodingSubProtocol: "http",
				TranscodingContainer: "mp4",
				MediaStreams: []MediaStream{
					{Type: "Video", Codec: "h264", Index: 0, IsDefault: true},
					{Type: "Audio", Codec: "aac", Index: 1, IsDefault: true, Channels: 2},
				},
			},
		},
		"PlaySessionId": itemID[:16],
	})
}

func (s *Server) videoStream(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)

	ctx := r.Context()
	stream, err := s.streams.GetByID(ctx, streamID)
	if err == nil && stream != nil && stream.URL != "" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Transfer-Encoding", "chunked")

		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-analyzeduration", "2000000",
			"-probesize", "2000000",
			"-i", stream.URL,
			"-c:v", "copy",
			"-c:a", "aac",
			"-b:a", "192k",
			"-ac", "2",
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			"-f", "mp4",
			"pipe:1",
		)
		cmd.Stdout = w
		cmd.Stderr = nil

		if err := cmd.Run(); err != nil {
			s.log.Debug().Err(err).Str("stream", streamID).Msg("ffmpeg stream ended")
		}
		return
	}

	http.Error(w, "stream not found", http.StatusNotFound)
}

func (s *Server) liveTvInfo(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Services":     []any{},
		"IsEnabled":    true,
		"EnabledUsers": []string{},
	})
}

func (s *Server) liveTvChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.channels.List(r.Context())
	if err != nil {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	var items []BaseItemDto
	for _, ch := range channels {
		item := BaseItemDto{
			Name:          ch.Name,
			ServerID:      s.serverID,
			ID:            strings.ReplaceAll(ch.ID, "-", ""),
			Type:          "LiveTvChannel",
			MediaType:     "Video",
			IsFolder:      false,
			ChannelNumber: ch.ID[:8],
			ImageTags:     map[string]string{},
			UserData:      &UserItemData{Key: ch.ID},
		}

		if ch.Logo != "" {
			item.ImageTags["Primary"] = "logo"
		}

		items = append(items, item)
	}

	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (s *Server) liveTvPrograms(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items:            []BaseItemDto{},
		TotalRecordCount: 0,
	})
}

func (s *Server) liveTvGuideInfo(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	s.respondJSON(w, http.StatusOK, map[string]any{
		"StartDate": now.Format(time.RFC3339),
		"EndDate":   now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
	})
}

func (s *Server) streamToItem(st *models.Stream) BaseItemDto {
	itemID := strings.ReplaceAll(st.ID, "-", "")
	item := BaseItemDto{
		Name:         st.Name,
		ServerID:     s.serverID,
		ID:           itemID,
		Type:         "Video",
		MediaType:    "Video",
		IsFolder:     false,
		LocationType: "FileSystem",
		ImageTags:    map[string]string{},
		UserData:     &UserItemData{Key: st.ID},
		MediaSources: []MediaSource{
			{
				Protocol:             "Http",
				ID:                   itemID,
				Type:                 "Default",
				Name:                 st.Name,
				IsRemote:             true,
				SupportsTranscoding:  true,
				SupportsDirectStream: true,
				SupportsDirectPlay:   false,
				IsInfiniteStream:     false,
				Container:            "mp4",
				MediaStreams: []MediaStream{
					{
						Type:    "Video",
						Codec:   "h264",
						Index:   0,
						IsDefault: true,
					},
					{
						Type:      "Audio",
						Codec:     "aac",
						Index:     1,
						IsDefault: true,
						Channels:  2,
					},
				},
				TranscodingURL:         fmt.Sprintf("/Videos/%s/stream.mp4?static=true", itemID),
				TranscodingSubProtocol: "http",
				TranscodingContainer:   "mp4",
			},
		},
	}
	if st.VODType == "movie" {
		item.Type = "Movie"
	}
	if st.VODDuration > 0 {
		item.RunTimeTicks = int64(st.VODDuration * 10000000)
		item.MediaSources[0].RunTimeTicks = item.RunTimeTicks
	}
	return item
}

func hashString(s string) uint32 {
	var h uint32
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}

func addDashes(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) == 32 {
		return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	}
	return id
}

