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
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
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

	filters := strings.Join(append(q["filters"], q["Filters"]...), ",")
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

		if searchTerm != "" {
			nameMatch := strings.Contains(strings.ToLower(st.Name), searchTerm)
			collMatch := st.VODCollection != "" && strings.Contains(strings.ToLower(st.VODCollection), searchTerm)
			if !nameMatch && !collMatch {
				continue
			}
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

func (s *Server) lookupCast(name, mediaType string) []PersonDto {
	clean, year := tmdb.BuildQuery(name)
	query := clean
	if year != "" {
		query = clean + " (" + year + ")"
	}
	mt := mediaType
	if mt == "series" {
		mt = "tv"
	}

	result, err := s.tmdbClient.Search(query, mt)
	if err != nil || result == nil {
		return nil
	}
	results, ok := result["results"].([]any)
	if !ok || len(results) == 0 {
		return nil
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return nil
	}
	id := int(0)
	if v, ok := first["id"].(float64); ok {
		id = int(v)
	}
	if id == 0 {
		return nil
	}

	details, err := s.tmdbClient.Details(mt, fmt.Sprintf("%d", id))
	if err != nil {
		return nil
	}
	dm, ok := details.(map[string]any)
	if !ok {
		return nil
	}
	credits, ok := dm["credits"].(map[string]any)
	if !ok {
		return nil
	}
	cast, ok := credits["cast"].([]any)
	if !ok {
		return nil
	}

	var people []PersonDto
	for _, c := range cast {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := cm["name"].(string)
		character, _ := cm["character"].(string)
		pid := 0
		if v, ok := cm["id"].(float64); ok {
			pid = int(v)
		}
		people = append(people, PersonDto{
			Name: name,
			ID:   fmt.Sprintf("person_%d", pid),
			Role: character,
			Type: "Actor",
		})
		if len(people) >= 20 {
			break
		}
	}
	return people
}

func sortName(name string) string {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

func (s *Server) enrichMovieItem(st *models.Stream) BaseItemDto {
	itemID := strings.ReplaceAll(st.ID, "-", "")
	item := BaseItemDto{
		Name:         st.Name,
		SortName:     sortName(st.Name),
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
				MediaStreams: s.buildMediaStreams(st),
			},
		},
	}

	if st.VODDuration > 0 {
		item.RunTimeTicks = int64(st.VODDuration * 10000000)
		item.MediaSources[0].RunTimeTicks = item.RunTimeTicks
	}

	if st.VODRes != "" {
		switch strings.ToLower(st.VODRes) {
		case "4k", "2160p":
			item.Width, item.Height = 3840, 2160
		case "1080p":
			item.Width, item.Height = 1920, 1080
		case "720p":
			item.Width, item.Height = 1280, 720
		}
	}

	if m := s.tmdbClient.LookupMovie(st.Name); m != nil {
		item.Overview = m.Overview
		item.CommunityRating = m.Rating
		item.OfficialRating = m.Certification
		item.Genres = m.Genres
		for _, g := range m.Genres {
			item.GenreItems = append(item.GenreItems, NameIDPair{Name: g, ID: fmt.Sprintf("genre_%x", hashString(g))})
		}
		if m.Year != "" {
			if yr, _ := strconv.Atoi(m.Year); yr > 0 {
				item.ProductionYear = yr
				item.PremiereDate = m.Year + "-01-01T00:00:00.0000000Z"
				item.DateCreated = m.Year + "-01-01T00:00:00.0000000Z"
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
		SortName:     sortName(name),
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
		for _, g := range sr.Genres {
			item.GenreItems = append(item.GenreItems, NameIDPair{Name: g, ID: fmt.Sprintf("genre_%x", hashString(g))})
		}
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
		if stream.VODType == "series" {
			item.Type = "Episode"
			key := stream.VODSeries
			if key == "" {
				key = stream.Name
			}
			item.SeriesName = key
			item.SeriesID = fmt.Sprintf("series_%x", hashString(key))
			item.IndexNumber = stream.VODEpisode
			item.ParentIndexNumber = stream.VODSeason
			if ep := s.tmdbClient.LookupEpisode(key, stream.VODSeason, stream.VODEpisode); ep != nil {
				if ep.Name != "" {
					item.Name = ep.Name
				}
				if ep.Overview != "" {
					item.Overview = ep.Overview
				}
			}
		}
		item.People = s.lookupCast(stream.Name, stream.VODType)
		s.respondJSON(w, http.StatusOK, item)
		return
	}

	if strings.HasPrefix(itemID, "series_") {
		streams, _ := s.streams.List(ctx)
		for _, st := range streams {
			if st.VODType != "series" {
				continue
			}
			key := st.VODSeries
			if key == "" {
				key = st.Name
			}
			if fmt.Sprintf("series_%x", hashString(key)) == itemID {
				item := s.enrichSeriesItem(key)
				var childCount int
				for _, s2 := range streams {
					if s2.VODType == "series" {
						k2 := s2.VODSeries
						if k2 == "" {
							k2 = s2.Name
						}
						if k2 == key {
							childCount++
						}
					}
				}
				item.ChildCount = childCount
				s.respondJSON(w, http.StatusOK, item)
				return
			}
		}
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
	case viewTVID:
		items = s.buildSeriesItems(ctx, "", "")
	default:
		items = s.buildMovieItems(ctx, "", "")
	}

	sortItems(items, "PremiereDate", "Descending")

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

func (s *Server) getSuggestions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items := s.buildMovieItems(ctx, "", "")
	sortItems(items, "Random", "")
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	if len(items) > limit {
		items = items[:limit]
	}
	if items == nil {
		items = []BaseItemDto{}
	}
	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
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

	seasonEpCount := make(map[int]int)
	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := st.VODSeries
		if key == "" {
			key = st.Name
		}
		if fmt.Sprintf("series_%x", hashString(key)) == seriesID {
			seasonEpCount[st.VODSeason]++
		}
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
			ChildCount:        seasonEpCount[num],
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
	seen := make(map[string]bool)
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

		dedup := fmt.Sprintf("s%de%d", st.VODSeason, st.VODEpisode)
		if seen[dedup] {
			continue
		}
		seen[dedup] = true

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
			if ep.StillPath != "" {
				item.ImageTags["Primary"] = "tmdb_ep"
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
	itemID := chi.URLParam(r, "itemId")
	ctx := r.Context()

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err != nil || stream == nil {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	var sourceGenres []string
	if m := s.tmdbClient.LookupMovie(stream.Name); m != nil {
		sourceGenres = m.Genres
	}

	if len(sourceGenres) == 0 {
		s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: []BaseItemDto{}, TotalRecordCount: 0})
		return
	}

	allMovies := s.buildMovieItems(ctx, "", "")
	var similar []BaseItemDto
	for _, item := range allMovies {
		if item.ID == itemID {
			continue
		}
		overlap := 0
		for _, g := range item.Genres {
			for _, sg := range sourceGenres {
				if g == sg {
					overlap++
				}
			}
		}
		if overlap >= 2 {
			similar = append(similar, item)
		}
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	if len(similar) > limit {
		similar = similar[:limit]
	}
	if similar == nil {
		similar = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: similar, TotalRecordCount: len(similar)})
}

func (s *Server) getSpecialFeatures(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, []BaseItemDto{})
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	imageType := chi.URLParam(r, "imageType")
	ctx := r.Context()

	isBackdrop := strings.EqualFold(imageType, "Backdrop")

	if strings.HasPrefix(itemID, "series_") {
		streams, _ := s.streams.List(ctx)
		for _, st := range streams {
			if st.VODType != "series" {
				continue
			}
			key := st.VODSeries
			if key == "" {
				key = st.Name
			}
			if fmt.Sprintf("series_%x", hashString(key)) == itemID {
				if isBackdrop {
					if bd := s.tmdbClient.LookupBackdrop(key, "series"); bd != "" {
						r.URL, _ = url.Parse(fmt.Sprintf("/api/tmdb/image?size=w1280&path=%s", url.QueryEscape(bd)))
						s.tmdbClient.ServeImage(w, r)
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
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err == nil && stream != nil {
		lookupName := stream.Name
		mediaType := stream.VODType
		if stream.VODType == "series" && stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}

		if stream.VODType == "series" && stream.VODSeason > 0 && stream.VODEpisode > 0 {
			if ep := s.tmdbClient.LookupEpisode(lookupName, stream.VODSeason, stream.VODEpisode); ep != nil && ep.StillPath != "" {
				r.URL, _ = url.Parse(fmt.Sprintf("/api/tmdb/image?size=w300&path=%s", url.QueryEscape(ep.StillPath)))
				s.tmdbClient.ServeImage(w, r)
				return
			}
		}

		if isBackdrop {
			backdrop := s.tmdbClient.LookupBackdrop(lookupName, mediaType)
			if backdrop != "" {
				r.URL, _ = url.Parse(fmt.Sprintf("/api/tmdb/image?size=w1280&path=%s", url.QueryEscape(backdrop)))
				s.tmdbClient.ServeImage(w, r)
				return
			}
		}

		posterURL := s.tmdbClient.LookupPoster(lookupName, mediaType)
		if posterURL != "" {
			r.URL, _ = url.Parse(posterURL)
			s.tmdbClient.ServeImage(w, r)
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

	w.WriteHeader(http.StatusNotFound)
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

	var mediaStreams []MediaStream
	if stream != nil {
		mediaStreams = s.buildMediaStreams(stream)
	} else {
		mediaStreams = []MediaStream{
			{Type: "Video", Codec: videoCodec, Index: 0, IsDefault: true, Width: 1920, Height: 1080},
			{Type: "Audio", Codec: audioCodec, Index: 1, IsDefault: true, Channels: 2, SampleRate: 48000},
		}
	}

	ms := MediaSource{
		Protocol: "Http", ID: itemID, Type: "Default", Name: "Default",
		Container: container, IsRemote: true,
		SupportsTranscoding:    true,
		SupportsDirectStream:   true,
		SupportsDirectPlay:     false,
		RunTimeTicks:           ticks,
		DefaultAudioStreamIndex: 1,
		TranscodingURL:         fmt.Sprintf("/Videos/%s/stream.mp4?static=true", itemID),
		TranscodingSubProtocol: "http",
		TranscodingContainer:   "mp4",
		MediaStreams:            mediaStreams,
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

	channel, chErr := s.channels.GetByID(ctx, streamID)
	if chErr == nil && channel != nil {
		channelURL := fmt.Sprintf("%s:8080/channel/%s?profile=Browser", s.baseURL, streamID)
		http.Redirect(w, r, channelURL, http.StatusTemporaryRedirect)
		return
	}

	stream, err := s.streams.GetByID(ctx, streamID)
	if err != nil || stream == nil || stream.URL == "" {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Connection", "keep-alive")

	args := []string{
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
	}

	s.log.Info().Str("stream", streamID).Str("url", stream.URL).Msg("starting jellyfin video stream")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = w

	if err := cmd.Run(); err != nil {
		s.log.Debug().Err(err).Str("stream", streamID).Msg("jellyfin video stream ended")
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
	for i, ch := range channels {
		chID := strings.ReplaceAll(ch.ID, "-", "")
		item := BaseItemDto{
			Name: ch.Name, ServerID: s.serverID,
			ID: chID, Type: "LiveTvChannel",
			MediaType: "Video", IsFolder: false,
			ChannelNumber: fmt.Sprintf("%d", i+1),
			ImageTags: map[string]string{}, UserData: &UserItemData{Key: ch.ID},
			MediaSources: []MediaSource{
				{
					Protocol: "Http", ID: chID, Type: "Default",
					Name: ch.Name, IsRemote: true, IsInfiniteStream: true,
					SupportsTranscoding: true, SupportsDirectStream: true,
					TranscodingURL: fmt.Sprintf("/Videos/%s/stream.mp4?static=true", chID),
				},
			},
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
	ctx := r.Context()
	channels, _ := s.channels.List(ctx)
	epgData, _ := s.epg.ListEPGData(ctx)

	epgByChannel := make(map[string]string)
	var epgIDs []string
	for _, e := range epgData {
		epgByChannel[e.ChannelID] = e.ID
		epgIDs = append(epgIDs, e.ID)
	}

	programs, _ := s.epg.ListProgramsByEPGDataIDs(ctx, epgIDs)

	var items []BaseItemDto
	now := time.Now()

	for _, ch := range channels {
		epgID := epgByChannel[ch.TvgID]
		if epgID == "" {
			continue
		}
		progs := programs[epgID]
		for _, p := range progs {
			if p.Stop.Before(now.Add(-2*time.Hour)) || p.Start.After(now.Add(24*time.Hour)) {
				continue
			}

			chID := strings.ReplaceAll(ch.ID, "-", "")
			item := BaseItemDto{
				Name:     p.Title,
				ServerID: s.serverID,
				ID:       fmt.Sprintf("prog_%s_%d", chID, p.Start.Unix()),
				Type:     "LiveTvProgram",
				Overview: p.Description,
				ParentID: chID,
			}

			if !p.Start.IsZero() {
				item.PremiereDate = p.Start.Format(time.RFC3339)
			}
			if !p.Start.IsZero() && !p.Stop.IsZero() {
				item.RunTimeTicks = int64(p.Stop.Sub(p.Start).Seconds() * 10000000)
			}

			items = append(items, item)
		}
	}

	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
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
			si := items[i].SortName
			if si == "" { si = items[i].Name }
			sj := items[j].SortName
			if sj == "" { sj = items[j].Name }
			less = strings.ToLower(si) < strings.ToLower(sj)
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

func (s *Server) buildMediaStreams(st *models.Stream) []MediaStream {
	videoCodec := "h264"
	audioCodec := "aac"
	width, height := 1920, 1080
	channels := 2

	if st.VODVCodec != "" {
		vc := strings.ToLower(st.VODVCodec)
		switch {
		case vc == "hevc" || vc == "h265":
			videoCodec = "hevc"
		case vc == "h264" || vc == "avc":
			videoCodec = "h264"
		case vc == "av1":
			videoCodec = "av1"
		default:
			videoCodec = vc
		}
	}
	if st.VODACodec != "" {
		ac := strings.ToLower(st.VODACodec)
		switch {
		case strings.Contains(ac, "aac"):
			audioCodec = "aac"
		case strings.Contains(ac, "ac3") || strings.Contains(ac, "ac-3"):
			audioCodec = "ac3"
		case strings.Contains(ac, "eac3") || strings.Contains(ac, "e-ac-3"):
			audioCodec = "eac3"
		case strings.Contains(ac, "dts"):
			audioCodec = "dca"
		case strings.Contains(ac, "truehd"):
			audioCodec = "truehd"
		case strings.Contains(ac, "flac"):
			audioCodec = "flac"
		default:
			audioCodec = ac
		}
	}
	if st.VODRes != "" {
		switch strings.ToLower(st.VODRes) {
		case "4k", "2160p":
			width, height = 3840, 2160
		case "1080p":
			width, height = 1920, 1080
		case "720p":
			width, height = 1280, 720
		case "480p":
			width, height = 854, 480
		}
	}
	if st.VODAudio != "" {
		au := strings.ToLower(st.VODAudio)
		if strings.Contains(au, "7.1") {
			channels = 8
		} else if strings.Contains(au, "5.1") || strings.Contains(au, "atmos") {
			channels = 6
		}
	}

	return []MediaStream{
		{Type: "Video", Codec: videoCodec, Index: 0, IsDefault: true, Width: width, Height: height},
		{Type: "Audio", Codec: audioCodec, Index: 1, IsDefault: true, Channels: channels, SampleRate: 48000},
	}
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
