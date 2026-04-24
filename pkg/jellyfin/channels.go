package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type epgLookup struct {
	byTvgID    map[string]string
	programs   map[string][]models.ProgramData
	fetchedAt  time.Time
}

func (s *Server) loadEPG(ctx context.Context) epgLookup {
	epgData, _ := s.epg.ListEPGData(ctx)
	byTvgID := make(map[string]string, len(epgData))
	var epgIDs []string
	for _, e := range epgData {
		byTvgID[e.ChannelID] = e.ID
		epgIDs = append(epgIDs, e.ID)
	}
	programs, _ := s.epg.ListProgramsByEPGDataIDs(ctx, epgIDs)
	return epgLookup{byTvgID: byTvgID, programs: programs, fetchedAt: time.Now()}
}

func (e *epgLookup) nowPlaying(tvgID string) *models.ProgramData {
	epgID, ok := e.byTvgID[tvgID]
	if !ok {
		return nil
	}
	now := e.fetchedAt
	for i := range e.programs[epgID] {
		p := &e.programs[epgID][i]
		if now.After(p.Start) && now.Before(p.Stop) {
			return p
		}
	}
	return nil
}

func channelHasLogo(ch *models.Channel) bool {
	return ch.LogoID != nil || ch.Logo != ""
}

func newChannelItem(ch *models.Channel, serverID string) BaseItemDto {
	chID := stripDashes(ch.ID)
	item := BaseItemDto{
		Name: ch.Name, ServerID: serverID,
		ID: chID, Type: "Video",
		MediaType: "Video", IsFolder: false,
		ImageTags: map[string]string{},
		UserData:  &UserItemData{Key: ch.ID},
		MediaSources: []MediaSource{{
			Protocol: "Http", ID: chID, Type: "Default",
			Name: ch.Name, IsRemote: true, IsInfiniteStream: true,
			SupportsTranscoding: true, SupportsDirectStream: true,
			TranscodingURL: channelStreamURL(chID),
		}},
	}
	if channelHasLogo(ch) {
		item.ImageTags["Primary"] = "logo"
	}
	return item
}

func newLiveTvChannelItem(ch *models.Channel, index int, serverID string) BaseItemDto {
	chID := stripDashes(ch.ID)
	item := BaseItemDto{
		Name: ch.Name, ServerID: serverID,
		ID: chID, Type: "LiveTvChannel",
		MediaType: "Video", IsFolder: false,
		ChannelNumber: fmt.Sprintf("%d", index+1),
		ImageTags:     map[string]string{},
		UserData:      &UserItemData{Key: ch.ID},
		MediaSources: []MediaSource{{
			Protocol: "Http", ID: chID, Type: "Default",
			Name: ch.Name, IsRemote: true, IsInfiniteStream: true,
			SupportsTranscoding: true, SupportsDirectStream: true,
			TranscodingURL: channelStreamURL(chID),
		}},
	}
	if channelHasLogo(ch) {
		item.ImageTags["Primary"] = "logo"
		item.ChannelPrimaryImageTag = "logo"
	}
	return item
}

func (s *Server) rootFolderID() string {
	h := hashString(s.serverID)
	return fmt.Sprintf("e9d5%04x%08x%016x", h>>16, h, uint64(h)*6364136223846793005)[:32]
}

func (s *Server) newCollectionFolderItem(name, id, collectionType string, imgTags map[string]string) BaseItemDto {
	dashlessID := stripDashes(id)
	dashedID := addDashes(dashlessID)
	return BaseItemDto{
		Name:                     name,
		ServerID:                 s.serverID,
		ID:                       dashlessID,
		DateCreated:              "2026-01-01T00:00:00.0000000Z",
		DateLastMediaAdded:       "0001-01-01T00:00:00.0000000Z",
		CanDelete:                boolPtr(false),
		CanDownload:              boolPtr(false),
		SortName:                 strings.ToLower(name),
		ExternalUrls:             []any{},
		EnableMediaSourceDisplay: true,
		Taglines:                 []string{},
		Genres:                   []string{},
		PlayAccess:               "Full",
		RemoteTrailers:           []any{},
		ProviderIds:              map[string]string{},
		IsFolder:                 true,
		ParentID:                 s.rootFolderID(),
		Type:                     "CollectionFolder",
		CollectionType:           collectionType,
		People:                   []PersonDto{},
		Studios:                  []NameIDPair{},
		GenreItems:               []NameIDPair{},
		UserData: &UserItemData{
			Key:    dashedID,
			ItemID: dashlessID,
		},
		SpecialFeatureCount:  0,
		DisplayPreferencesId: dashlessID,
		Tags:                 []string{},
		ImageTags:            imgTags,
		BackdropImageTags:    []string{},
		ImageBlurHashes:      map[string]any{},
		LocationType:         "FileSystem",
		MediaType:            "Unknown",
		LockedFields:         []string{},
		LockData:             boolPtr(false),
	}
}

func (s *Server) userViews(w http.ResponseWriter, r *http.Request) {
	views := []BaseItemDto{
		s.newCollectionFolderItem("Movies", viewMoviesID, "movies", map[string]string{}),
		s.newCollectionFolderItem("TV Shows", viewTVID, "tvshows", map[string]string{}),
	}

	groupHasLogo := make(map[string]bool)
	if s.channels != nil {
		channels, _ := s.channels.List(r.Context())
		for _, ch := range channels {
			if ch.ChannelGroupID != nil && (ch.LogoID != nil || ch.Logo != "") {
				groupHasLogo[*ch.ChannelGroupID] = true
			}
		}
	}

	var groups []models.ChannelGroup
	if s.channelGroups != nil {
		groups, _ = s.channelGroups.List(r.Context())
	}
	for _, g := range groups {
		if !g.JellyfinEnabled {
			continue
		}
		colType := g.JellyfinType
		if colType == "" {
			colType = "folders"
		}
		imgTags := map[string]string{}
		if g.ImageURL != "" || groupHasLogo[g.ID] {
			imgTags["Primary"] = "logo"
		}
		views = append(views, s.newCollectionFolderItem(g.Name, groupItemID(g.ID), colType, imgTags))
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{
		Items: views, TotalRecordCount: len(views),
	})
}

func (s *Server) groupChannels(w http.ResponseWriter, r *http.Request, groupID string) {
	ctx := r.Context()
	channels, err := s.channels.List(ctx)
	if err != nil {
		s.respondJSON(w, http.StatusOK, emptyResult())
		return
	}

	epg := s.loadEPG(ctx)

	var items []BaseItemDto
	for _, ch := range channels {
		if ch.ChannelGroupID == nil || *ch.ChannelGroupID != groupID {
			continue
		}
		item := newChannelItem(&ch, s.serverID)
		if p := epg.nowPlaying(ch.TvgID); p != nil {
			item.Overview = p.Title + " — " + p.Description
		}
		items = append(items, item)
	}
	if items == nil {
		items = []BaseItemDto{}
	}
	s.paginateAndRespond(w, r, items)
}

func (s *Server) liveTvInfo(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]any{
		"Services": []any{}, "IsEnabled": true, "EnabledUsers": []string{},
	})
}

func (s *Server) liveTvChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.channels.List(r.Context())
	if err != nil {
		s.respondJSON(w, http.StatusOK, emptyResult())
		return
	}

	epg := s.loadEPG(r.Context())

	var items []BaseItemDto
	for i, ch := range channels {
		item := newLiveTvChannelItem(&ch, i, s.serverID)
		if p := epg.nowPlaying(ch.TvgID); p != nil {
			item.CurrentProgram = &BaseItemDto{
				Name:     p.Title,
				Overview: p.Description,
				ID:       fmt.Sprintf("prog_%s_%d", stripDashes(ch.ID), p.Start.Unix()),
				Type:     "LiveTvProgram",
			}
			if !p.Start.IsZero() {
				item.CurrentProgram.PremiereDate = p.Start.Format(time.RFC3339)
			}
			if !p.Start.IsZero() && !p.Stop.IsZero() {
				item.CurrentProgram.RunTimeTicks = durationToTicks(p.Stop.Sub(p.Start))
			}
		}
		items = append(items, item)
	}
	if items == nil {
		items = []BaseItemDto{}
	}

	s.respondJSON(w, http.StatusOK, BaseItemDtoQueryResult{Items: items, TotalRecordCount: len(items)})
}

func (s *Server) liveTvPrograms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	isAiring := q.Get("isAiring") == "true"
	hasAired := q.Get("hasAired")
	isMovie := q.Get("isMovie")
	isSeries := q.Get("isSeries")
	isNews := q.Get("isNews")
	isKids := q.Get("isKids")
	isSports := q.Get("isSports")
	limit := 50
	if l := q.Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}

	ctx := r.Context()
	channels, _ := s.channels.List(ctx)
	epg := s.loadEPG(ctx)
	now := epg.fetchedAt

	var items []BaseItemDto
	for _, ch := range channels {
		epgID, ok := epg.byTvgID[ch.TvgID]
		if !ok {
			continue
		}
		for _, p := range epg.programs[epgID] {
			if isAiring && !(now.After(p.Start) && now.Before(p.Stop)) {
				continue
			}
			if hasAired == "false" && p.Stop.Before(now) {
				continue
			}
			if p.Stop.Before(now.Add(-1*time.Hour)) || p.Start.After(now.Add(24*time.Hour)) {
				continue
			}

			cat := strings.ToLower(p.Category)
			if isMovie == "true" && !strings.Contains(cat, "movie") && !strings.Contains(cat, "film") {
				continue
			}
			if isSeries == "true" && !strings.Contains(cat, "series") && !strings.Contains(cat, "drama") && !strings.Contains(cat, "soap") {
				continue
			}
			if isNews == "true" && !strings.Contains(cat, "news") {
				continue
			}
			if isKids == "true" && !strings.Contains(cat, "kid") && !strings.Contains(cat, "child") && !strings.Contains(cat, "cartoon") && !strings.Contains(cat, "animation") {
				continue
			}
			if isSports == "true" && !strings.Contains(cat, "sport") {
				continue
			}

			chID := stripDashes(ch.ID)
			item := BaseItemDto{
				Name:          p.Title,
				ServerID:      s.serverID,
				ID:            fmt.Sprintf("prog_%s_%d", chID, p.Start.Unix()),
				Type:          "LiveTvProgram",
				Overview:      p.Description,
				ParentID:      chID,
				ChannelNumber: ch.Name,
				ImageTags:     map[string]string{},
			}
			if ch.Logo != "" {
				item.ChannelPrimaryImageTag = "logo"
			}
			if !p.Start.IsZero() {
				item.PremiereDate = p.Start.Format(time.RFC3339)
			}
			if !p.Start.IsZero() && !p.Stop.IsZero() {
				item.RunTimeTicks = durationToTicks(p.Stop.Sub(p.Start))
			}

			items = append(items, item)
			if len(items) >= limit {
				break
			}
		}
		if len(items) >= limit {
			break
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
