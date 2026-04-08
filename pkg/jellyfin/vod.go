package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

const (
	viewMoviesID = "f0000000-0000-0000-0000-000000000001"
	viewTVID     = "f0000000-0000-0000-0000-000000000002"
)

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	parentID := firstOf(q, "parentId", "ParentId")
	itemTypes := strings.Join(append(q["includeItemTypes"], q["IncludeItemTypes"]...), ",")
	searchTerm := strings.ToLower(firstOf(q, "searchTerm", "SearchTerm"))
	genres := firstOf(q, "genres", "Genres")
	sortBy := firstOf(q, "sortBy", "SortBy")
	sortOrder := firstOf(q, "sortOrder", "SortOrder")

	ctx := r.Context()

	filters := strings.Join(append(q["filters"], q["Filters"]...), ",")
	if strings.Contains(filters, "IsFavorite") {
		s.respondJSON(w, http.StatusOK, emptyResult())
		return
	}

	if strings.HasPrefix(parentID, "group_") {
		groupID := addDashes(strings.TrimPrefix(parentID, "group_"))
		s.groupChannels(w, r, groupID)
		return
	}

	hasMovies := parentID == viewMoviesID || strings.Contains(itemTypes, "Movie") || strings.Contains(itemTypes, "BoxSet") || strings.Contains(itemTypes, "MusicVideo") || strings.Contains(itemTypes, "Video")
	hasSeries := parentID == viewTVID || strings.Contains(itemTypes, "Series")

	var items []BaseItemDto
	if hasMovies || (!hasSeries && searchTerm != "") {
		items = append(items, s.buildMovieItems(ctx, searchTerm, genres)...)
	}
	if hasSeries || (!hasMovies && searchTerm != "") {
		items = append(items, s.buildSeriesItems(ctx, searchTerm, genres)...)
	}

	if !hasMovies && !hasSeries && searchTerm == "" {
		s.respondJSON(w, http.StatusOK, emptyResult())
		return
	}

	sortItems(items, sortBy, sortOrder)
	s.paginateAndRespond(w, r, items)
}

func (s *Server) itemDetail(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	ctx := r.Context()

	if itemID == viewMoviesID || itemID == viewTVID || strings.HasPrefix(itemID, "group_") {
		s.userViews(w, r)
		return
	}

	if stream, err := s.streams.GetByID(ctx, addDashes(itemID)); err == nil && stream != nil {
		item := s.enrichMovieItem(stream)
		if stream.VODType == "series" {
			item.Type = "Episode"
			key := seriesKey(stream)
			item.SeriesName = key
			item.SeriesID = seriesIDFromName(key)
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
		if item, ok := s.findSeriesItem(ctx, itemID); ok {
			s.respondJSON(w, http.StatusOK, item)
			return
		}
	}

	if channel, err := s.channels.GetByID(ctx, addDashes(itemID)); err == nil && channel != nil {
		item := newChannelItem(channel, s.serverID)
		s.respondJSON(w, http.StatusOK, item)
		return
	}

	s.respondJSON(w, http.StatusOK, BaseItemDto{
		Name: "Unknown", ServerID: s.serverID, ID: itemID, Type: "Video",
	})
}

func (s *Server) findSeriesItem(ctx context.Context, seriesID string) (BaseItemDto, bool) {
	streams, _ := s.streams.List(ctx)
	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := seriesKey(&st)
		if seriesIDFromName(key) != seriesID {
			continue
		}
		item := s.enrichSeriesItem(key)
		var childCount int
		for _, s2 := range streams {
			if s2.VODType == "series" && seriesKey(&s2) == key {
				childCount++
			}
		}
		item.ChildCount = childCount
		return item, true
	}
	return BaseItemDto{}, false
}

func (s *Server) latestItems(w http.ResponseWriter, r *http.Request) {
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
		items = []BaseItemDto{}
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

func (s *Server) listSuggestions(w http.ResponseWriter, r *http.Request) {
	items := s.buildMovieItems(r.Context(), "", "")
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

func (s *Server) listResumeItems(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, emptyResult())
}

func (s *Server) listSeasons(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "seriesId")
	streams, _ := s.streams.List(r.Context())

	seasonSet := make(map[int]bool)
	seasonEpCount := make(map[int]int)
	var name string

	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := seriesKey(&st)
		if seriesIDFromName(key) != targetID {
			continue
		}
		name = key
		if st.VODSeason > 0 {
			seasonSet[st.VODSeason] = true
		}
		seasonEpCount[st.VODSeason]++
	}

	if len(seasonSet) == 0 {
		seasonSet[1] = true
	}

	var items []BaseItemDto
	for num := range seasonSet {
		items = append(items, BaseItemDto{
			Name:         fmt.Sprintf("Season %d", num),
			ServerID:     s.serverID,
			ID:           fmt.Sprintf("%s_s%d", targetID, num),
			Type:         "Season",
			SeriesName:   name,
			SeriesID:     targetID,
			IndexNumber:  num,
			IsFolder:     true,
			ChildCount:   seasonEpCount[num],
			ImageTags:    map[string]string{},
			UserData:     &UserItemData{Key: fmt.Sprintf("%s_s%d", targetID, num)},
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].IndexNumber < items[j].IndexNumber
	})

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
}

func (s *Server) listEpisodes(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "seriesId")
	seasonNum, _ := strconv.Atoi(r.URL.Query().Get("seasonId"))
	if seasonNum == 0 {
		if sn := r.URL.Query().Get("season"); sn != "" {
			seasonNum, _ = strconv.Atoi(sn)
		}
	}

	streams, _ := s.streams.List(r.Context())
	seen := make(map[string]bool)
	var items []BaseItemDto

	for _, st := range streams {
		if st.VODType != "series" {
			continue
		}
		key := seriesKey(&st)
		if seriesIDFromName(key) != targetID {
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
		item.SeriesID = targetID
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

func (s *Server) listFilters(w http.ResponseWriter, r *http.Request) {
	streams, _ := s.streams.List(r.Context())

	genreSet := make(map[string]bool)
	yearSet := make(map[int]bool)
	ratingSet := make(map[string]bool)

	for _, st := range streams {
		if st.VODType != "movie" {
			continue
		}
		if m := s.tmdbClient.LookupMovie(st.Name); m != nil {
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

func (s *Server) listSimilarItems(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	ctx := r.Context()

	stream, err := s.streams.GetByID(ctx, addDashes(itemID))
	if err != nil || stream == nil {
		s.respondJSON(w, http.StatusOK, emptyResult())
		return
	}

	var sourceGenres []string
	if m := s.tmdbClient.LookupMovie(stream.Name); m != nil {
		sourceGenres = m.Genres
	}
	if len(sourceGenres) == 0 {
		s.respondJSON(w, http.StatusOK, emptyResult())
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
		key := seriesKey(&st)
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
	itemID := stripDashes(st.ID)

	container := "mp4"
	if st.URL != "" {
		if idx := strings.LastIndex(st.URL, "."); idx >= 0 {
			switch strings.ToLower(st.URL[idx+1:]) {
			case "mkv":
				container = "mkv"
			case "avi":
				container = "avi"
			case "ts":
				container = "ts"
			}
		}
	}

	item := BaseItemDto{
		Name:         st.Name,
		SortName:     sortName(st.Name),
		Container:    container,
		ServerID:     s.serverID,
		ID:           itemID,
		Type:         "Movie",
		MediaType:    "Video",
		IsFolder:     false,
		LocationType: "FileSystem",
		ImageTags:    map[string]string{},
		UserData:     &UserItemData{Key: st.ID},
		MediaSources: []MediaSource{{
			Protocol: "Http", ID: itemID, Type: "Default", Name: st.Name,
			Container: "mp4", IsRemote: true, SupportsTranscoding: true,
			SupportsDirectStream: true, SupportsDirectPlay: false,
			TranscodingURL:         channelStreamURL(itemID),
			TranscodingSubProtocol: "http", TranscodingContainer: "mp4",
			MediaStreams: s.buildMediaStreams(st),
		}},
	}

	if st.VODDuration > 0 {
		item.RunTimeTicks = secondsToTicks(st.VODDuration)
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
		item.GenreItems = genreItems(m.Genres)
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

	if item.ProductionYear == 0 && st.VODYear > 0 {
		item.ProductionYear = st.VODYear
		item.PremiereDate = fmt.Sprintf("%d-01-01T00:00:00.0000000Z", st.VODYear)
	}

	return item
}

func (s *Server) enrichSeriesItem(name string) BaseItemDto {
	item := BaseItemDto{
		Name:         name,
		SortName:     sortName(name),
		ServerID:     s.serverID,
		ID:           seriesIDFromName(name),
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
		item.GenreItems = genreItems(sr.Genres)
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
	tmdbID := 0
	if v, ok := first["id"].(float64); ok {
		tmdbID = int(v)
	}
	if tmdbID == 0 {
		return nil
	}

	details, err := s.tmdbClient.Details(mt, fmt.Sprintf("%d", tmdbID))
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
		actorName, _ := cm["name"].(string)
		character, _ := cm["character"].(string)
		pid := 0
		if v, ok := cm["id"].(float64); ok {
			pid = int(v)
		}
		people = append(people, PersonDto{
			Name: actorName,
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
