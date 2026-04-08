package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

const (
	viewMoviesID = "f0000000-0000-0000-0000-000000000001"
	viewTVID     = "f0000000-0000-0000-0000-000000000002"
	viewLiveTVID = "f0000000-0000-0000-0000-000000000004"
)

func (s *Server) userViews(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items: []BaseItemDto{
			{Name: "Movies", ServerID: s.serverID, ID: viewMoviesID, Type: "CollectionFolder", CollectionType: "movies", IsFolder: true, ImageTags: map[string]string{}},
			{Name: "TV Shows", ServerID: s.serverID, ID: viewTVID, Type: "CollectionFolder", CollectionType: "tvshows", IsFolder: true, ImageTags: map[string]string{}},
			{Name: "Live TV", ServerID: s.serverID, ID: viewLiveTVID, Type: "CollectionFolder", CollectionType: "livetv", IsFolder: true, ImageTags: map[string]string{}},
		},
		TotalRecordCount: 3,
	})
}

func (s *Server) getItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	parentID := firstOf(q, "parentId", "ParentId")
	itemTypes := strings.Join(append(q["includeItemTypes"], q["IncludeItemTypes"]...), ",")
	searchTerm := strings.ToLower(firstOf(q, "searchTerm", "SearchTerm"))
	genres := firstOf(q, "genres", "Genres")
	sortBy := firstOf(q, "sortBy", "SortBy")
	sortOrder := firstOf(q, "sortOrder", "SortOrder")
	recursive := firstOf(q, "recursive", "Recursive") == "true"
	_ = recursive

	ctx := r.Context()

	filters := firstOf(q, "filters", "Filters")
	if strings.Contains(filters, "IsFavorite") {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	hasMovies := parentID == viewMoviesID || strings.Contains(itemTypes, "Movie") || strings.Contains(itemTypes, "BoxSet") || strings.Contains(itemTypes, "MusicVideo") || strings.Contains(itemTypes, "Video")
	hasSeries := parentID == viewTVID || strings.Contains(itemTypes, "Series")
	hasLiveTV := parentID == viewLiveTVID

	if hasLiveTV {
		s.liveTvChannels(w, r)
		return
	}

	var items []BaseItemDto
	if hasMovies || (!hasSeries && searchTerm != "") {
		items = append(items, s.buildMovieItems(ctx, searchTerm, genres)...)
	}
	if hasSeries || (!hasMovies && searchTerm != "") {
		items = append(items, s.buildSeriesItems(ctx, searchTerm, genres)...)
	}

	if !hasMovies && !hasSeries && searchTerm == "" {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	sortItems(items, sortBy, sortOrder)
	s.paginateAndRespond(w, r, items)
}

func (s *Server) buildMovieItems(ctx context.Context, searchTerm, genres string) []BaseItemDto {
	streams, err := s.streams.List(ctx)
	if err != nil {
		return nil
	}

	genreFilter := parseGenres(genres)
	var items []BaseItemDto

	for _, st := range streams {
		if st.VODType != "movie" {
			continue
		}

		if searchTerm != "" && !strings.Contains(strings.ToLower(st.Name), searchTerm) {
			continue
		}

		item := s.enrichMovieItem(&st)

		if len(genreFilter) > 0 && !matchesGenres(item.Genres, genreFilter) {
			continue
		}

		items = append(items, item)
	}

	return items
}

func (s *Server) buildSeriesItems(ctx context.Context, searchTerm, genres string) []BaseItemDto {
	streams, err := s.streams.List(ctx)
	if err != nil {
		return nil
	}

	genreFilter := parseGenres(genres)
	seriesMap := make(map[string]*BaseItemDto)

	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := st.VODSeries
		if key == "" {
			key = st.Name
		}
		if searchTerm != "" && !strings.Contains(strings.ToLower(key), searchTerm) {
			continue
		}
		if existing, ok := seriesMap[key]; ok {
			existing.ChildCount++
			continue
		}

		item := s.enrichSeriesItem(key)
		item.ChildCount = 1

		if len(genreFilter) > 0 && !matchesGenres(item.Genres, genreFilter) {
			continue
		}

		seriesMap[key] = &item
	}

	var items []BaseItemDto
	for _, item := range seriesMap {
		items = append(items, *item)
	}
	return items
}

func (s *Server) enrichMovieItem(st *models.Stream) BaseItemDto {
	itemID := strings.ReplaceAll(st.ID, "-", "")
	item := BaseItemDto{
		Name:         st.Name,
		ServerID:     s.serverID,
		ID:           itemID,
		Type:         "Movie",
		MediaType:    "Video",
		IsFolder:     false,
		LocationType: "FileSystem",
		ImageTags:    map[string]string{},
		UserData:     &UserItemData{Key: st.ID},
		MediaSources: []MediaSource{
			{
				Protocol: "Http", ID: itemID, Type: "Default", Name: st.Name,
				Container: "mp4", IsRemote: true, SupportsTranscoding: true,
				SupportsDirectStream: true, SupportsDirectPlay: false,
				TranscodingURL: fmt.Sprintf("/Videos/%s/stream.mp4?static=true", itemID),
				TranscodingSubProtocol: "http", TranscodingContainer: "mp4",
				MediaStreams: []MediaStream{
					{Type: "Video", Codec: "h264", Index: 0, IsDefault: true},
					{Type: "Audio", Codec: "aac", Index: 1, IsDefault: true, Channels: 2},
				},
			},
		},
	}

	if st.VODDuration > 0 {
		item.RunTimeTicks = int64(st.VODDuration * 10000000)
		item.MediaSources[0].RunTimeTicks = item.RunTimeTicks
	}

	lookupName := st.Name
	if st.VODCollection != "" {
		lookupName = st.Name
	}

	if m := s.tmdbClient.LookupMovie(lookupName); m != nil {
		item.Overview = m.Overview
		item.CommunityRating = m.Rating
		item.OfficialRating = m.Certification
		item.Genres = m.Genres
		if m.Year != "" {
			if yr, _ := strconv.Atoi(m.Year); yr > 0 {
				item.ProductionYear = yr
				item.PremiereDate = m.Year + "-01-01T00:00:00.0000000Z"
			}
		}
		if m.PosterPath != "" {
			item.ImageTags["Primary"] = "tmdb"
		}
		if m.BackdropPath != "" {
			item.BackdropImageTags = []string{"tmdb"}
		}
	}

	return item
}

func (s *Server) enrichSeriesItem(name string) BaseItemDto {
	item := BaseItemDto{
		Name:         name,
		ServerID:     s.serverID,
		ID:           fmt.Sprintf("series_%x", hashString(name)),
		Type:         "Series",
		MediaType:    "Video",
		IsFolder:     true,
		LocationType: "FileSystem",
		ImageTags:    map[string]string{},
		UserData:     &UserItemData{Key: name},
	}

	if sr := s.tmdbClient.LookupSeries(name); sr != nil {
		item.Overview = sr.Overview
		item.CommunityRating = sr.Rating
		item.OfficialRating = sr.Certification
		item.Genres = sr.Genres
		if sr.Year != "" {
			if yr, _ := strconv.Atoi(sr.Year); yr > 0 {
				item.ProductionYear = yr
				item.PremiereDate = sr.Year + "-01-01T00:00:00.0000000Z"
			}
		}
		if sr.PosterPath != "" {
			item.ImageTags["Primary"] = "tmdb"
		}
		if sr.BackdropPath != "" {
			item.BackdropImageTags = []string{"tmdb"}
		}
	}

	return item
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
		item := s.enrichMovieItem(stream)
		s.respondJSON(w, http.StatusOK, item)
		return
	}

	channel, err := s.channels.GetByID(ctx, addDashes(itemID))
	if err == nil && channel != nil {
		item := BaseItemDto{
			Name: channel.Name, ServerID: s.serverID,
			ID: itemID, Type: "LiveTvChannel", MediaType: "Video",
			ImageTags: map[string]string{},
		}
		if channel.Logo != "" {
			item.ImageTags["Primary"] = "logo"
		}
		s.respondJSON(w, http.StatusOK, item)
		return
	}

	s.respondJSON(w, http.StatusOK, BaseItemDto{
		Name: "Unknown", ServerID: s.serverID, ID: itemID, Type: "Video",
	})
}

func (s *Server) getLatest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	parentID := firstOf(q, "parentId", "ParentId")
	ctx := r.Context()

	var items []BaseItemDto
	switch parentID {
	case viewMoviesID:
		items = s.buildMovieItems(ctx, "", "")
		sortItems(items, "DateCreated", "Descending")
	case viewTVID:
		items = s.buildSeriesItems(ctx, "", "")
		sortItems(items, "DateCreated", "Descending")
	}

	limit := 20
	if l := firstOf(q, "limit", "Limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, items)
}

func (s *Server) getResume(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
}

func (s *Server) getSeasons(w http.ResponseWriter, r *http.Request) {
	seriesID := chi.URLParam(r, "seriesId")
	ctx := r.Context()

	streams, _ := s.streams.List(ctx)
	seasonSet := make(map[int]bool)
	var seriesName string

	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := st.VODSeries
		if key == "" {
			key = st.Name
		}
		if fmt.Sprintf("series_%x", hashString(key)) == seriesID {
			seriesName = key
			if st.VODSeason > 0 {
				seasonSet[st.VODSeason] = true
			}
		}
	}

	if len(seasonSet) == 0 {
		seasonSet[1] = true
	}

	var items []BaseItemDto
	for num := range seasonSet {
		items = append(items, BaseItemDto{
			Name:              fmt.Sprintf("Season %d", num),
			ServerID:          s.serverID,
			ID:                fmt.Sprintf("%s_s%d", seriesID, num),
			Type:              "Season",
			SeriesName:        seriesName,
			SeriesID:          seriesID,
			IndexNumber:       num,
			IsFolder:          true,
			ImageTags:         map[string]string{},
			UserData:          &UserItemData{Key: fmt.Sprintf("%s_s%d", seriesID, num)},
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].IndexNumber < items[j].IndexNumber
	})

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
}

func (s *Server) getEpisodes(w http.ResponseWriter, r *http.Request) {
	seriesID := chi.URLParam(r, "seriesId")
	seasonNum, _ := strconv.Atoi(r.URL.Query().Get("seasonId"))
	if seasonNum == 0 {
		if sn := r.URL.Query().Get("season"); sn != "" {
			seasonNum, _ = strconv.Atoi(sn)
		}
	}
	ctx := r.Context()

	streams, _ := s.streams.List(ctx)
	var items []BaseItemDto

	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := st.VODSeries
		if key == "" {
			key = st.Name
		}
		if fmt.Sprintf("series_%x", hashString(key)) != seriesID {
			continue
		}
		if seasonNum > 0 && st.VODSeason != seasonNum {
			continue
		}

		item := s.enrichMovieItem(&st)
		item.Type = "Episode"
		item.SeriesName = key
		item.SeriesID = seriesID
		item.IndexNumber = st.VODEpisode
		item.ParentIndexNumber = st.VODSeason

		if ep := s.tmdbClient.LookupEpisode(key, st.VODSeason, st.VODEpisode); ep != nil {
			if ep.Name != "" {
				item.Name = ep.Name
			}
			if ep.Overview != "" {
				item.Overview = ep.Overview
			}
		}

		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ParentIndexNumber != items[j].ParentIndexNumber {
			return items[i].ParentIndexNumber < items[j].ParentIndexNumber
		}
		return items[i].IndexNumber < items[j].IndexNumber
	})

	if items == nil {
		items = []BaseItemDto{}
	}
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
}

func (s *Server) getFilters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streams, _ := s.streams.List(ctx)

	genreSet := make(map[string]bool)
	yearSet := make(map[int]bool)
	ratingSet := make(map[string]bool)

	for _, st := range streams {
		if st.VODType == "" {
			continue
		}
		lookupName := st.Name
		if st.VODType == "series" && st.VODSeries != "" {
			lookupName = st.VODSeries
		}
		if st.VODType == "movie" {
			if m := s.tmdbClient.LookupMovie(lookupName); m != nil {
				for _, g := range m.Genres {
					genreSet[g] = true
				}
				if m.Year != "" {
					if yr, _ := strconv.Atoi(m.Year); yr > 0 {
						yearSet[yr] = true
					}
				}
				if m.Certification != "" {
					ratingSet[m.Certification] = true
				}
			}
		}
	}

	var genreList []string
	for g := range genreSet {
		genreList = append(genreList, g)
	}
	sort.Strings(genreList)

	var yearList []int
	for y := range yearSet {
		yearList = append(yearList, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(yearList)))

	var ratingList []string
	for r := range ratingSet {
		ratingList = append(ratingList, r)
	}
	sort.Strings(ratingList)

	s.respondJSON(w, http.StatusOK, map[string]any{
		"Genres":          genreList,
		"Tags":            []string{},
		"OfficialRatings": ratingList,
		"Years":           yearList,
	})
}

func (s *Server) getSimilar(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
}

func (s *Server) getSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	imageType := chi.URLParam(r, "imageType")
	ctx := r.Context()

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err == nil && stream != nil {
		lookupName := stream.Name
		mediaType := stream.VODType
		if stream.VODType == "series" && stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}

		if strings.EqualFold(imageType, "Backdrop") || strings.EqualFold(imageType, "backdrop") {
			backdrop := s.tmdbClient.LookupBackdrop(lookupName, mediaType)
			if backdrop != "" {
				imgURL := fmt.Sprintf("%s:8080/api/tmdb/image?size=w1280&path=%s", s.baseURL, url.QueryEscape(backdrop))
				http.Redirect(w, r, imgURL, http.StatusTemporaryRedirect)
				return
			}
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
	streamID := addDashes(itemID)

	ctx := r.Context()
	stream, _ := s.streams.GetByID(ctx, streamID)

	container := "mp4"
	videoCodec := "h264"
	audioCodec := "aac"
	var ticks int64

	if stream != nil {
		if stream.VODVCodec != "" {
			vc := strings.ToLower(stream.VODVCodec)
			if vc == "h264" || vc == "avc" {
				videoCodec = "h264"
			} else if vc == "hevc" || vc == "h265" {
				videoCodec = "hevc"
			}
		}
		if stream.VODDuration > 0 {
			ticks = int64(stream.VODDuration * 10000000)
		}
	}

	ms := MediaSource{
		Protocol: "Http", ID: itemID, Type: "Default", Name: "Default",
		Container: container, IsRemote: true,
		SupportsTranscoding:  true,
		SupportsDirectStream: true,
		SupportsDirectPlay:   false,
		RunTimeTicks:         ticks,
		TranscodingURL:         fmt.Sprintf("/Videos/%s/stream.mp4?static=true", itemID),
		TranscodingSubProtocol: "http",
		TranscodingContainer:   "mp4",
		MediaStreams: []MediaStream{
			{Type: "Video", Codec: videoCodec, Index: 0, IsDefault: true, Width: 1920, Height: 1080},
			{Type: "Audio", Codec: audioCodec, Index: 1, IsDefault: true, Channels: 2, SampleRate: 48000},
		},
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"MediaSources":  []MediaSource{ms},
		"PlaySessionId": itemID[:min(16, len(itemID))],
	})
}

func (s *Server) videoStream(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	streamID := addDashes(itemID)

	ctx := r.Context()
	stream, err := s.streams.GetByID(ctx, streamID)
	if err != nil || stream == nil || stream.URL == "" {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

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

	if err := cmd.Run(); err != nil {
		s.log.Debug().Err(err).Str("stream", streamID).Msg("ffmpeg stream ended")
	}
}

func (s *Server) liveTvInfo(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Services": []any{}, "IsEnabled": true, "EnabledUsers": []string{},
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
			Name: ch.Name, ServerID: s.serverID,
			ID: strings.ReplaceAll(ch.ID, "-", ""), Type: "LiveTvChannel",
			MediaType: "Video", IsFolder: false,
			ChannelNumber: ch.ID[:8],
			ImageTags: map[string]string{}, UserData: &UserItemData{Key: ch.ID},
		}
		if ch.Logo != "" {
			item.ImageTags["Primary"] = "logo"
		}
		items = append(items, item)
	}
	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
}

func (s *Server) liveTvPrograms(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
}

func (s *Server) liveTvGuideInfo(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	s.respondJSON(w, http.StatusOK, map[string]any{
		"StartDate": now.Format(time.RFC3339),
		"EndDate":   now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
	})
}

func (s *Server) paginateAndRespond(w http.ResponseWriter, r *http.Request, items []BaseItemDto) {
	if items == nil {
		items = []BaseItemDto{}
	}
	total := len(items)

	startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	limit := total
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}

	if startIndex >= total {
		items = []BaseItemDto{}
	} else if startIndex+limit > total {
		items = items[startIndex:]
	} else {
		items = items[startIndex : startIndex+limit]
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items: items, TotalRecordCount: total, StartIndex: startIndex,
	})
}

func sortItems(items []BaseItemDto, sortBy, sortOrder string) {
	if sortBy == "" {
		sortBy = "SortName"
	}
	desc := strings.EqualFold(sortOrder, "Descending")

	sort.Slice(items, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "DateCreated", "PremiereDate":
			less = items[i].PremiereDate < items[j].PremiereDate
		case "CommunityRating":
			less = items[i].CommunityRating < items[j].CommunityRating
		case "Random":
			less = hashString(items[i].ID)%1000 < hashString(items[j].ID)%1000
		default:
			less = strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		if desc {
			return !less
		}
		return less
	})
}

func parseGenres(genres string) []string {
	if genres == "" {
		return nil
	}
	return strings.Split(genres, ",")
}

func matchesGenres(itemGenres, filter []string) bool {
	for _, f := range filter {
		found := false
		for _, g := range itemGenres {
			if strings.EqualFold(g, strings.TrimSpace(f)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func firstOf(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
