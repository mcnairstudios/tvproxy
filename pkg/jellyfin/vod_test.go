package jellyfin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type fakeStreamReader struct {
	streams []models.Stream
}

func (f *fakeStreamReader) List(_ context.Context) ([]models.Stream, error) {
	return f.streams, nil
}
func (f *fakeStreamReader) ListSummaries(_ context.Context) ([]models.StreamSummary, error) {
	return nil, nil
}
func (f *fakeStreamReader) ListByAccountID(_ context.Context, _ string) ([]models.Stream, error) {
	return nil, nil
}
func (f *fakeStreamReader) ListBySatIPSourceID(_ context.Context, _ string) ([]models.Stream, error) {
	return nil, nil
}
func (f *fakeStreamReader) ListByHDHRSourceID(_ context.Context, _ string) ([]models.Stream, error) {
	return nil, nil
}
func (f *fakeStreamReader) ListByVODType(_ context.Context, vodType string) ([]models.Stream, error) {
	var out []models.Stream
	for _, s := range f.streams {
		if s.VODType == vodType {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeStreamReader) GetByID(_ context.Context, id string) (*models.Stream, error) {
	for i, s := range f.streams {
		if s.ID == id {
			return &f.streams[i], nil
		}
	}
	return nil, nil
}

var _ store.StreamReader = (*fakeStreamReader)(nil)

func newVODTestServer(t *testing.T, streams []models.Stream) *Server {
	t.Helper()
	tmdbClient := tmdb.NewClient(t.TempDir(), func() string { return "" }, zerolog.Nop())
	return &Server{
		serverID:   "61da34e701b54548a25c783de5e13284",
		streams:    &fakeStreamReader{streams: streams},
		tmdbClient: tmdbClient,
		log:        zerolog.Nop(),
	}
}

func movieStream() models.Stream {
	return models.Stream{
		ID:          "f842a4f8-d071-cf6b-33e0-2873ba437ed7",
		Name:        "3 10 to Yuma",
		URL:         "http://example.com/video.mkv",
		VODType:     "movie",
		CacheType:   "local",
		VODYear:     2007,
		VODDuration: 7345.472,
	}
}

func TestEnrichMovieItem_FieldsMatchReference(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	if item.Name != "3 10 to Yuma" {
		t.Errorf("Name: got %q want %q", item.Name, "3 10 to Yuma")
	}
	if item.ServerID != "61da34e701b54548a25c783de5e13284" {
		t.Errorf("ServerId: got %q", item.ServerID)
	}
	if item.ID != "f842a4f8d071cf6b33e02873ba437ed7" {
		t.Errorf("Id: got %q want no-dash form", item.ID)
	}
	if item.Container != "mkv" {
		t.Errorf("Container: got %q want mkv", item.Container)
	}
	if item.ChannelID != nil {
		t.Errorf("ChannelId must be null, got %v", item.ChannelID)
	}
	if item.RunTimeTicks != secondsToTicks(7345.472) {
		t.Errorf("RunTimeTicks: got %d", item.RunTimeTicks)
	}
	if item.ProductionYear != 2007 {
		t.Errorf("ProductionYear: got %d want 2007", item.ProductionYear)
	}
	if item.IsFolder {
		t.Error("IsFolder must be false")
	}
	if item.Type != "Movie" {
		t.Errorf("Type: got %q want Movie", item.Type)
	}
	if item.VideoType != "VideoFile" {
		t.Errorf("VideoType: got %q want VideoFile", item.VideoType)
	}
	if item.ImageTags == nil {
		t.Error("ImageTags must not be nil")
	}
	if item.BackdropImageTags == nil {
		t.Error("BackdropImageTags must not be nil")
	}
	if item.ImageBlurHashes == nil {
		t.Error("ImageBlurHashes must not be nil")
	}
	if item.LocationType != "FileSystem" {
		t.Errorf("LocationType: got %q want FileSystem", item.LocationType)
	}
	if item.MediaType != "Video" {
		t.Errorf("MediaType: got %q want Video", item.MediaType)
	}
	if item.UserData == nil {
		t.Fatal("UserData must not be nil")
	}
	if item.UserData.Key != st.ID {
		t.Errorf("UserData.Key: got %q want %q", item.UserData.Key, st.ID)
	}
	if item.UserData.ItemID != item.ID {
		t.Errorf("UserData.ItemId: got %q want %q", item.UserData.ItemID, item.ID)
	}
}

func TestEnrichMovieItem_NoExtraFields(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	if item.Overview != "" {
		t.Errorf("Overview must be empty in list view, got %q", item.Overview)
	}
	if len(item.Genres) != 0 {
		t.Errorf("Genres must be empty in list view, got %v", item.Genres)
	}
	if item.CommunityRating != 0 {
		t.Errorf("CommunityRating must be zero in list view, got %v", item.CommunityRating)
	}
	if item.OfficialRating != "" {
		t.Errorf("OfficialRating must be empty in list view, got %q", item.OfficialRating)
	}
	if len(item.MediaSources) != 0 {
		t.Errorf("MediaSources must be absent in list view, got %v", item.MediaSources)
	}
	if len(item.People) != 0 {
		t.Errorf("People must be absent in list view, got %v", item.People)
	}
	if len(item.Studios) != 0 {
		t.Errorf("Studios must be absent in list view, got %v", item.Studios)
	}
	if len(item.GenreItems) != 0 {
		t.Errorf("GenreItems must be absent in list view, got %v", item.GenreItems)
	}
	if len(item.Taglines) != 0 {
		t.Errorf("Taglines must be absent in list view, got %v", item.Taglines)
	}
	if item.SortName != "" {
		t.Errorf("SortName must be absent in list view, got %q", item.SortName)
	}
	if item.DateCreated != "" {
		t.Errorf("DateCreated must be absent in list view, got %q", item.DateCreated)
	}
}

func TestEnrichMovieItem_JSONSerialisation(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{"Name", "ServerId", "Id", "Container", "ChannelId", "RunTimeTicks",
		"ProductionYear", "IsFolder", "Type", "UserData", "VideoType",
		"ImageTags", "BackdropImageTags", "ImageBlurHashes", "LocationType", "MediaType"}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("required field %q missing from JSON", field)
		}
	}

	forbidden := []string{"MediaSources", "SortName", "DateCreated", "Width", "Height"}
	for _, field := range forbidden {
		if raw, ok := m[field]; ok && string(raw) != "null" {
			t.Errorf("forbidden field %q present with non-null value in JSON output", field)
		}
	}

	nullOrAbsent := []string{"Overview", "CommunityRating", "OfficialRating",
		"Genres", "People", "Studios", "GenreItems", "Taglines"}
	for _, field := range nullOrAbsent {
		if raw, ok := m[field]; ok && string(raw) != "null" {
			t.Errorf("list-view field %q must be absent or null, got %s", field, raw)
		}
	}

	var channelID *string
	if err := json.Unmarshal(m["ChannelId"], &channelID); err != nil {
		t.Errorf("ChannelId unmarshal: %v", err)
	}
	if channelID != nil {
		t.Errorf("ChannelId must be JSON null, got %q", *channelID)
	}

	var videoType string
	json.Unmarshal(m["VideoType"], &videoType)
	if videoType != "VideoFile" {
		t.Errorf("VideoType: got %q want VideoFile", videoType)
	}

	var imageTags map[string]string
	json.Unmarshal(m["ImageTags"], &imageTags)
	if imageTags == nil {
		t.Error("ImageTags must be a JSON object, not null")
	}

	var backdropTags []string
	json.Unmarshal(m["BackdropImageTags"], &backdropTags)
	if backdropTags == nil {
		t.Error("BackdropImageTags must be a JSON array, not null")
	}

	var blurHashes map[string]any
	json.Unmarshal(m["ImageBlurHashes"], &blurHashes)
	if blurHashes == nil {
		t.Error("ImageBlurHashes must be a JSON object, not null")
	}
}

func TestEnrichMovieItem_UserDataFields(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	if item.UserData == nil {
		t.Fatal("UserData is nil")
	}

	data, _ := json.Marshal(item.UserData)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	for _, field := range []string{"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "Key", "ItemId"} {
		if _, ok := m[field]; !ok {
			t.Errorf("UserData missing required field %q", field)
		}
	}

	var pos int64
	json.Unmarshal(m["PlaybackPositionTicks"], &pos)
	if pos != 0 {
		t.Errorf("PlaybackPositionTicks: got %d want 0", pos)
	}
	var played bool
	json.Unmarshal(m["Played"], &played)
	if played {
		t.Error("Played must be false")
	}
}

func TestEnrichMovieItem_ContainerDetection(t *testing.T) {
	s := newVODTestServer(t, nil)

	cases := []struct {
		url  string
		want string
	}{
		{"http://example.com/video.mkv", "mkv"},
		{"http://example.com/video.avi", "avi"},
		{"http://example.com/video.ts", "ts"},
		{"http://example.com/video.mp4", "mp4"},
		{"http://example.com/video", "mp4"},
	}

	for _, tc := range cases {
		st := movieStream()
		st.URL = tc.url
		item := s.enrichMovieItem(&st)
		if item.Container != tc.want {
			t.Errorf("URL %q: Container got %q want %q", tc.url, item.Container, tc.want)
		}
	}
}

func TestEnrichMovieItem_PremiereDate(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	st.VODYear = 1999
	item := s.enrichMovieItem(&st)

	if item.ProductionYear != 1999 {
		t.Errorf("ProductionYear: got %d want 1999", item.ProductionYear)
	}
	if item.PremiereDate == "" {
		t.Error("PremiereDate must not be empty when year is set")
	}
	_, err := time.Parse("2006-01-02T15:04:05.0000000Z", item.PremiereDate)
	if err != nil {
		t.Errorf("PremiereDate format invalid: %v", err)
	}
}

func TestEnrichMovieItem_NoYearNoPremiereDate(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	st.VODYear = 0
	item := s.enrichMovieItem(&st)

	if item.ProductionYear != 0 {
		t.Errorf("ProductionYear: got %d want 0 when no year", item.ProductionYear)
	}
	if item.PremiereDate != "" {
		t.Errorf("PremiereDate must be empty when no year, got %q", item.PremiereDate)
	}
}

func TestEnrichMovieItem_ImageTagsEmptyWhenNoPoster(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	if len(item.ImageTags) != 0 {
		t.Errorf("ImageTags must be empty when no poster, got %v", item.ImageTags)
	}
}

func TestEnrichMovieItem_BackdropTagsAlwaysArray(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	data, _ := json.Marshal(item.BackdropImageTags)
	if string(data) == "null" {
		t.Error("BackdropImageTags must serialise as [] not null")
	}
	if len(item.BackdropImageTags) != 0 {
		t.Errorf("BackdropImageTags must be empty for movies without backdrop, got %v", item.BackdropImageTags)
	}
}

func TestEnrichMovieItem_NoDurationNoRunTimeTicks(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	st.VODDuration = 0
	item := s.enrichMovieItem(&st)

	if item.RunTimeTicks != 0 {
		t.Errorf("RunTimeTicks must be 0 when no duration, got %d", item.RunTimeTicks)
	}
}

func TestEnrichMovieItem_RunTimeTicksConversion(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	st.VODDuration = 7345.472
	item := s.enrichMovieItem(&st)

	expected := int64(7345.472 * 10000000)
	if item.RunTimeTicks != expected {
		t.Errorf("RunTimeTicks: got %d want %d", item.RunTimeTicks, expected)
	}
}

func TestBuildMovieItems_GenreFilterUsesDirectLookup(t *testing.T) {
	s := newVODTestServer(t, []models.Stream{
		{
			ID: "aaaaaaaa-bbbb-cccc-dddd-000000000001", Name: "Movie A",
			VODType: "movie", CacheType: "local",
		},
		{
			ID: "aaaaaaaa-bbbb-cccc-dddd-000000000002", Name: "Movie B",
			VODType: "movie", CacheType: "local",
		},
	})

	items := s.buildMovieItems(context.Background(), "", "Action")
	for _, item := range items {
		if len(item.Genres) != 0 {
			t.Errorf("item %q: Genres must be empty in list view, got %v", item.Name, item.Genres)
		}
	}
}

func TestBuildMovieItems_NonLocalExcluded(t *testing.T) {
	s := newVODTestServer(t, []models.Stream{
		{ID: "aaaaaaaa-0000-0000-0000-000000000001", Name: "Local", VODType: "movie", CacheType: "local"},
		{ID: "aaaaaaaa-0000-0000-0000-000000000002", Name: "Remote", VODType: "movie", CacheType: "remote"},
	})

	items := s.buildMovieItems(context.Background(), "", "")
	if len(items) != 1 {
		t.Errorf("expected 1 local movie, got %d", len(items))
	}
	if items[0].Name != "Local" {
		t.Errorf("expected Local, got %q", items[0].Name)
	}
}

func TestBuildMovieItems_SearchFilter(t *testing.T) {
	s := newVODTestServer(t, []models.Stream{
		{ID: "aaaaaaaa-0000-0000-0000-000000000001", Name: "The Dark Knight", VODType: "movie", CacheType: "local"},
		{ID: "aaaaaaaa-0000-0000-0000-000000000002", Name: "Inception", VODType: "movie", CacheType: "local"},
	})

	items := s.buildMovieItems(context.Background(), "dark", "")
	if len(items) != 1 {
		t.Errorf("expected 1 result, got %d", len(items))
	}
	if items[0].Name != "The Dark Knight" {
		t.Errorf("expected The Dark Knight, got %q", items[0].Name)
	}
}

func TestEnrichMovieItem_MatchesReferenceStructure(t *testing.T) {
	const referenceJSON = `{"Name":"3 10 to Yuma","ServerId":"61da34e701b54548a25c783de5e13284","Id":"f842a4f8d071cf6b33e02873ba437ed7","CanDelete":true,"HasSubtitles":true,"Container":"mkv","PremiereDate":"2021-04-23T10:03:17.0000000Z","ChannelId":null,"RunTimeTicks":73454720000,"ProductionYear":2007,"IsFolder":false,"Type":"Movie","UserData":{"PlaybackPositionTicks":0,"PlayCount":0,"IsFavorite":false,"Played":false,"Key":"f842a4f8-d071-cf6b-33e0-2873ba437ed7","ItemId":"f842a4f8d071cf6b33e02873ba437ed7"},"VideoType":"VideoFile","ImageTags":{},"BackdropImageTags":[],"ImageBlurHashes":{},"LocationType":"FileSystem","MediaType":"Video"}`

	var refItem map[string]json.RawMessage
	if err := json.Unmarshal([]byte(referenceJSON), &refItem); err != nil {
		t.Fatalf("parse reference: %v", err)
	}

	s := newVODTestServer(t, nil)
	st := movieStream()
	st.VODDuration = 7345.472
	item := s.enrichMovieItem(&st)

	data, _ := json.Marshal(item)
	var ourItem map[string]json.RawMessage
	json.Unmarshal(data, &ourItem)

	for field := range refItem {
		if _, ok := ourItem[field]; !ok {
			if field == "HasSubtitles" {
				continue
			}
			t.Errorf("our item missing reference field %q", field)
		}
	}

	for field, raw := range ourItem {
		if _, ok := refItem[field]; !ok {
			s := string(raw)
			if s == "null" || s == "false" || s == "0" {
				continue
			}
			t.Errorf("our item has extra field %q=%s not in reference", field, raw)
		}
	}
}

func TestEnrichMovieDetail_RequiredFieldsPresent(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	required := []string{
		"Name", "ServerId", "Id", "CanDelete", "Container", "ChannelId",
		"RunTimeTicks", "ProductionYear", "IsFolder", "Type", "UserData",
		"VideoType", "ImageTags", "BackdropImageTags", "ImageBlurHashes",
		"LocationType", "MediaType", "SortName", "CanDownload",
		"EnableMediaSourceDisplay", "PlayAccess",
		"ExternalUrls", "RemoteTrailers", "ProviderIds",
		"ProductionLocations", "Tags", "Studios", "Taglines",
		"Chapters", "LockedFields", "Trickplay",
		"MediaSources",
	}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("detail missing required field %q", field)
		}
	}
}

func TestEnrichMovieDetail_EmptyArrayFields(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	data, _ := json.Marshal(item)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	emptyArrayFields := []string{
		"ExternalUrls", "RemoteTrailers", "ProductionLocations",
		"Tags", "Taglines", "Chapters", "LockedFields",
	}
	for _, field := range emptyArrayFields {
		raw, ok := m[field]
		if !ok {
			t.Errorf("field %q missing", field)
			continue
		}
		if string(raw) != "[]" {
			t.Errorf("field %q: expected [], got %s", field, raw)
		}
	}
}

func TestEnrichMovieDetail_EmptyObjectFields(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	data, _ := json.Marshal(item)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	emptyObjectFields := []string{"ProviderIds", "Trickplay"}
	for _, field := range emptyObjectFields {
		raw, ok := m[field]
		if !ok {
			t.Errorf("field %q missing", field)
			continue
		}
		if string(raw) != "{}" {
			t.Errorf("field %q: expected {}, got %s", field, raw)
		}
	}
}

func TestEnrichMovieDetail_MediaSourcesPresent(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	if len(item.MediaSources) == 0 {
		t.Fatal("MediaSources must not be empty in detail view")
	}
	ms := item.MediaSources[0]
	if ms.Protocol == "" {
		t.Error("MediaSource.Protocol must not be empty")
	}
	if ms.ID == "" {
		t.Error("MediaSource.ID must not be empty")
	}
	if ms.Type == "" {
		t.Error("MediaSource.Type must not be empty")
	}
	if len(ms.MediaStreams) == 0 {
		t.Error("MediaSource.MediaStreams must not be empty")
	}
}

func TestEnrichMovieDetail_DetailFieldsNotPopulatedInListView(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieItem(&st)

	data, _ := json.Marshal(item)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	detailOnlyNonNull := []string{
		"MediaSources", "SortName", "EnableMediaSourceDisplay", "PlayAccess",
	}
	for _, field := range detailOnlyNonNull {
		if raw, ok := m[field]; ok && string(raw) != "null" {
			t.Errorf("detail-only field %q must not have non-null value in list view, got %s", field, raw)
		}
	}

	detailRequiresArrayOrObject := []string{
		"ExternalUrls", "RemoteTrailers", "ProviderIds", "ProductionLocations",
		"Tags", "Taglines", "Chapters", "LockedFields", "Trickplay",
	}
	for _, field := range detailRequiresArrayOrObject {
		if raw, ok := m[field]; ok {
			s := string(raw)
			if s != "null" && s != "[]" && s != "{}" {
				t.Errorf("field %q in list view should be null/[]/{}  got %s", field, raw)
			}
		}
	}
}

func TestEnrichMovieDetail_CanDeleteAndDownload(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	detail := s.enrichMovieDetail(&st)

	if detail.CanDelete == nil || !*detail.CanDelete {
		t.Error("CanDelete must be true in detail view")
	}
	if detail.CanDownload == nil || !*detail.CanDownload {
		t.Error("CanDownload must be true in detail view")
	}

	list := s.enrichMovieItem(&st)
	if list.CanDelete == nil || !*list.CanDelete {
		t.Error("CanDelete must be true in list view")
	}
	if list.CanDownload != nil && *list.CanDownload {
		t.Error("CanDownload must not be true in list view")
	}
}

func TestEnrichMovieDetail_PlayAccess(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	if item.PlayAccess != "Full" {
		t.Errorf("PlayAccess: got %q want Full", item.PlayAccess)
	}
}

func TestEnrichMovieDetail_StudiosEmptyArray(t *testing.T) {
	s := newVODTestServer(t, nil)
	st := movieStream()
	item := s.enrichMovieDetail(&st)

	data, _ := json.Marshal(item)
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	raw, ok := m["Studios"]
	if !ok {
		t.Fatal("Studios must be present in detail view")
	}
	if string(raw) != "[]" {
		t.Errorf("Studios: expected [], got %s", raw)
	}
}
