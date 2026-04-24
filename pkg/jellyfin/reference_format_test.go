package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var hex32RE = regexp.MustCompile(`^[0-9a-f]{32}$`)

func assertHex32(t *testing.T, label, v string) {
	t.Helper()
	if !hex32RE.MatchString(v) {
		t.Errorf("%s: want 32-char dashless hex, got %q", label, v)
	}
}

func assertStringSlice(t *testing.T, label string, v any) {
	t.Helper()
	if v == nil {
		t.Errorf("%s: must not be nil", label)
		return
	}
	if _, ok := v.([]any); !ok {
		t.Errorf("%s: must be array, got %T", label, v)
	}
}

func assertObjectField(t *testing.T, label string, v any) {
	t.Helper()
	if v == nil {
		t.Errorf("%s: must not be nil", label)
		return
	}
	if _, ok := v.(map[string]any); !ok {
		t.Errorf("%s: must be object, got %T", label, v)
	}
}

func TestUserViews_CollectionFolderFormat(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/UserViews", nil)
	w := httptest.NewRecorder()

	srv.userViews(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	items, ok := result["Items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("Items must be non-empty array")
	}

	if result["StartIndex"] == nil {
		t.Error("StartIndex must be present")
	}
	if result["TotalRecordCount"] == nil {
		t.Error("TotalRecordCount must be present")
	}

	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("item %d is not object", i)
		}
		validateCollectionFolderItem(t, item, i)
	}
}

func validateCollectionFolderItem(t *testing.T, item map[string]any, idx int) {
	t.Helper()
	label := func(f string) string {
		name, _ := item["Name"].(string)
		return "item[" + name + "]." + f
	}

	assertHex32(t, label("Id"), item["Id"].(string))

	if item["Type"] != "CollectionFolder" {
		t.Errorf("%s: want CollectionFolder, got %v", label("Type"), item["Type"])
	}
	if item["IsFolder"] != true {
		t.Errorf("%s: want true", label("IsFolder"))
	}

	requiredStringFields := []string{
		"Name", "ServerId", "Id", "SortName", "DateCreated", "DateLastMediaAdded",
		"PlayAccess", "LocationType", "MediaType", "CollectionType", "ParentId",
		"DisplayPreferencesId",
	}
	for _, f := range requiredStringFields {
		if item[f] == nil || item[f] == "" {
			t.Errorf("%s: must be present and non-empty", label(f))
		}
	}

	if item["DateLastMediaAdded"] != "0001-01-01T00:00:00.0000000Z" {
		t.Errorf("%s: want zero time, got %v", label("DateLastMediaAdded"), item["DateLastMediaAdded"])
	}
	if item["PlayAccess"] != "Full" {
		t.Errorf("%s: want Full, got %v", label("PlayAccess"), item["PlayAccess"])
	}
	if item["LocationType"] != "FileSystem" {
		t.Errorf("%s: want FileSystem, got %v", label("LocationType"), item["LocationType"])
	}
	if item["MediaType"] != "Unknown" {
		t.Errorf("%s: want Unknown, got %v", label("MediaType"), item["MediaType"])
	}
	if item["EnableMediaSourceDisplay"] != true {
		t.Errorf("%s: want true", label("EnableMediaSourceDisplay"))
	}
	if item["LockData"] != false {
		t.Errorf("%s: want false", label("LockData"))
	}
	if item["CanDelete"] != false {
		t.Errorf("%s: want false", label("CanDelete"))
	}
	if item["CanDownload"] != false {
		t.Errorf("%s: want false", label("CanDownload"))
	}

	// ChannelId must be present and null
	channelID, exists := item["ChannelId"]
	if !exists {
		t.Errorf("%s: must be present (even as null)", label("ChannelId"))
	} else if channelID != nil {
		t.Errorf("%s: must be null for CollectionFolder, got %v", label("ChannelId"), channelID)
	}

	// Arrays must be [] not null
	for _, f := range []string{"ExternalUrls", "Taglines", "Genres", "RemoteTrailers", "Tags", "LockedFields", "BackdropImageTags"} {
		assertStringSlice(t, label(f), item[f])
	}
	for _, f := range []string{"People", "Studios", "GenreItems"} {
		assertStringSlice(t, label(f), item[f])
	}

	// ProviderIds and ImageBlurHashes must be {}
	assertObjectField(t, label("ProviderIds"), item["ProviderIds"])
	assertObjectField(t, label("ImageBlurHashes"), item["ImageBlurHashes"])
	assertObjectField(t, label("ImageTags"), item["ImageTags"])

	// UserData must be object with Key, ItemId, PlaybackPositionTicks
	userData, ok := item["UserData"].(map[string]any)
	if !ok {
		t.Errorf("%s: must be object", label("UserData"))
	} else {
		if userData["Key"] == nil || userData["Key"] == "" {
			t.Errorf("%s: Key must be present", label("UserData.Key"))
		}
		if userData["ItemId"] == nil {
			t.Errorf("%s: ItemId must be present", label("UserData.ItemId"))
		}
		if _, ok := userData["PlaybackPositionTicks"]; !ok {
			t.Errorf("%s: PlaybackPositionTicks must be present", label("UserData.PlaybackPositionTicks"))
		}
		if _, ok := userData["PlayCount"]; !ok {
			t.Errorf("%s: PlayCount must be present", label("UserData.PlayCount"))
		}
		if _, ok := userData["IsFavorite"]; !ok {
			t.Errorf("%s: IsFavorite must be present", label("UserData.IsFavorite"))
		}
		if _, ok := userData["Played"]; !ok {
			t.Errorf("%s: Played must be present", label("UserData.Played"))
		}
	}

	// DisplayPreferencesId must match Id
	if item["DisplayPreferencesId"] != item["Id"] {
		t.Errorf("%s: DisplayPreferencesId %v must equal Id %v", label(""), item["DisplayPreferencesId"], item["Id"])
	}

	// ParentId must be a 32-char hex
	parentID, _ := item["ParentId"].(string)
	assertHex32(t, label("ParentId"), parentID)
}

func TestUserViews_MoviesAndTVPresent(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/UserViews", nil)
	w := httptest.NewRecorder()

	srv.userViews(w, req)

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)

	items := result["Items"].([]any)
	names := make(map[string]string)
	for _, raw := range items {
		item := raw.(map[string]any)
		name := item["Name"].(string)
		colType, _ := item["CollectionType"].(string)
		names[name] = colType
	}

	if names["Movies"] != "movies" {
		t.Errorf("Movies item: CollectionType want movies, got %q", names["Movies"])
	}
	if names["TV Shows"] != "tvshows" {
		t.Errorf("TV Shows item: CollectionType want tvshows, got %q", names["TV Shows"])
	}
}

func TestUserViews_IDsAre32CharHex(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/UserViews", nil)
	w := httptest.NewRecorder()

	srv.userViews(w, req)

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)

	items := result["Items"].([]any)
	for _, raw := range items {
		item := raw.(map[string]any)
		id, _ := item["Id"].(string)
		assertHex32(t, item["Name"].(string)+".Id", id)
	}
}

func TestUserViews_UserDataKeyIsDashedID(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/UserViews", nil)
	w := httptest.NewRecorder()

	srv.userViews(w, req)

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)

	items := result["Items"].([]any)
	for _, raw := range items {
		item := raw.(map[string]any)
		id, _ := item["Id"].(string)
		userData, _ := item["UserData"].(map[string]any)
		if userData == nil {
			t.Errorf("%s: UserData missing", item["Name"])
			continue
		}
		key, _ := userData["Key"].(string)
		itemID, _ := userData["ItemId"].(string)
		expectedKey := addDashes(id)
		if key != expectedKey {
			t.Errorf("%s: UserData.Key want %q (dashed), got %q", item["Name"], expectedKey, key)
		}
		if itemID != id {
			t.Errorf("%s: UserData.ItemId want %q, got %q", item["Name"], id, itemID)
		}
	}
}

func TestUserByID_JSONShape(t *testing.T) {
	nowJF := &JellyfinTime{}
	u := UserDto{
		Name:                  "admin",
		ServerID:              "61da34e701b54548a25c783de5e13284",
		ID:                    "d39ff01f781e46788ca024a8a59c2a19",
		HasPassword:           true,
		HasConfiguredPassword: true,
		LastLoginDate:         nowJF,
		LastActivityDate:      nowJF,
		Configuration:         defaultUserConfig(),
		Policy:                defaultPolicy(true),
	}

	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}

	if _, ok := m["ServerName"]; ok {
		t.Error("UserDto must NOT include ServerName field")
	}

	requiredTopLevel := []string{
		"Name", "ServerId", "Id", "HasPassword", "HasConfiguredPassword",
		"HasConfiguredEasyPassword", "EnableAutoLogin", "LastLoginDate",
		"LastActivityDate", "Configuration", "Policy",
	}
	for _, k := range requiredTopLevel {
		if _, ok := m[k]; !ok {
			t.Errorf("missing top-level field %q in UserDto JSON", k)
		}
	}

	cfg, ok := m["Configuration"].(map[string]any)
	if !ok {
		t.Fatal("Configuration must be object")
	}
	cfgArrayFields := []string{"GroupedFolders", "OrderedViews", "LatestItemsExcludes", "MyMediaExcludes"}
	for _, f := range cfgArrayFields {
		assertStringSlice(t, "Configuration."+f, cfg[f])
	}

	policy, ok := m["Policy"].(map[string]any)
	if !ok {
		t.Fatal("Policy must be object")
	}
	policyArrayFields := []string{
		"BlockedTags", "AllowedTags", "AccessSchedules", "BlockUnratedItems",
		"EnableContentDeletionFromFolders", "EnabledDevices", "EnabledChannels",
		"EnabledFolders", "BlockedMediaFolders", "BlockedChannels",
	}
	for _, f := range policyArrayFields {
		assertStringSlice(t, "Policy."+f, policy[f])
	}

	if policy["AuthenticationProviderId"] != "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider" {
		t.Errorf("Policy.AuthenticationProviderId wrong: %v", policy["AuthenticationProviderId"])
	}
	if policy["PasswordResetProviderId"] != "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider" {
		t.Errorf("Policy.PasswordResetProviderId wrong: %v", policy["PasswordResetProviderId"])
	}
	if policy["SyncPlayAccess"] != "CreateAndJoinGroups" {
		t.Errorf("Policy.SyncPlayAccess wrong: %v", policy["SyncPlayAccess"])
	}
	if policy["LoginAttemptsBeforeLockout"] != float64(-1) {
		t.Errorf("Policy.LoginAttemptsBeforeLockout: want -1, got %v", policy["LoginAttemptsBeforeLockout"])
	}
}

func TestDisplayPreferences_MatchesReference(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/3ce5b65d-e116-d731-65d1-efc4a30ec35c?client=emby", nil)
	req = withURLParam(req, "id", "3ce5b65d-e116-d731-65d1-efc4a30ec35c")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var prefs DisplayPreferences
	if err := json.NewDecoder(w.Body).Decode(&prefs); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if prefs.SortBy != "SortName" {
		t.Errorf("SortBy: want SortName, got %q", prefs.SortBy)
	}
	if prefs.RememberIndexing {
		t.Error("RememberIndexing: want false")
	}
	if prefs.PrimaryImageHeight != 250 {
		t.Errorf("PrimaryImageHeight: want 250, got %d", prefs.PrimaryImageHeight)
	}
	if prefs.PrimaryImageWidth != 250 {
		t.Errorf("PrimaryImageWidth: want 250, got %d", prefs.PrimaryImageWidth)
	}
	if prefs.ScrollDirection != "Horizontal" {
		t.Errorf("ScrollDirection: want Horizontal, got %q", prefs.ScrollDirection)
	}
	if !prefs.ShowBackdrop {
		t.Error("ShowBackdrop: want true")
	}
	if prefs.RememberSorting {
		t.Error("RememberSorting: want false")
	}
	if prefs.SortOrder != "Ascending" {
		t.Errorf("SortOrder: want Ascending, got %q", prefs.SortOrder)
	}
	if prefs.ShowSidebar {
		t.Error("ShowSidebar: want false")
	}
	if prefs.Client != "emby" {
		t.Errorf("Client: want emby, got %q", prefs.Client)
	}
	if prefs.CustomPrefs.ChromecastVersion != "stable" {
		t.Errorf("CustomPrefs.chromecastVersion: want stable, got %q", prefs.CustomPrefs.ChromecastVersion)
	}
	if prefs.CustomPrefs.SkipForwardLength != "30000" {
		t.Errorf("CustomPrefs.skipForwardLength: want 30000, got %q", prefs.CustomPrefs.SkipForwardLength)
	}
	if prefs.CustomPrefs.SkipBackLength != "10000" {
		t.Errorf("CustomPrefs.skipBackLength: want 10000, got %q", prefs.CustomPrefs.SkipBackLength)
	}
	if prefs.CustomPrefs.EnableNextVideoInfoOverlay != "False" {
		t.Errorf("CustomPrefs.enableNextVideoInfoOverlay: want False, got %q", prefs.CustomPrefs.EnableNextVideoInfoOverlay)
	}
	if prefs.CustomPrefs.TVHome != nil {
		t.Errorf("CustomPrefs.tvhome: want null, got %v", prefs.CustomPrefs.TVHome)
	}
	if prefs.CustomPrefs.DashboardTheme != nil {
		t.Errorf("CustomPrefs.dashboardTheme: want null, got %v", prefs.CustomPrefs.DashboardTheme)
	}
}

func TestDisplayPreferences_JSONKeys(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings?client=emby", nil)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var raw map[string]any
	json.NewDecoder(w.Body).Decode(&raw)

	// These are the exact keys from the reference display_preferences_usersettings.json
	referenceKeys := []string{
		"Id", "SortBy", "RememberIndexing", "PrimaryImageHeight", "PrimaryImageWidth",
		"CustomPrefs", "ScrollDirection", "ShowBackdrop", "RememberSorting",
		"SortOrder", "ShowSidebar", "Client",
	}
	for _, k := range referenceKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing reference key %q", k)
		}
	}

	customPrefs, ok := raw["CustomPrefs"].(map[string]any)
	if !ok {
		t.Fatal("CustomPrefs is not object")
	}
	referenceCustomKeys := []string{
		"chromecastVersion", "skipForwardLength", "skipBackLength",
		"enableNextVideoInfoOverlay", "tvhome", "dashboardTheme",
	}
	for _, k := range referenceCustomKeys {
		if _, ok := customPrefs[k]; !ok {
			t.Errorf("missing CustomPrefs reference key %q", k)
		}
	}
}

func TestCollectionFolderItem_SortNameIsLowercase(t *testing.T) {
	srv := newTestServer()
	item := srv.newCollectionFolderItem("Movies", viewMoviesID, "movies", map[string]string{})
	if item.SortName != "movies" {
		t.Errorf("SortName: want movies, got %q", item.SortName)
	}

	item2 := srv.newCollectionFolderItem("TV Shows", viewTVID, "tvshows", map[string]string{})
	if item2.SortName != "tv shows" {
		t.Errorf("SortName for 'TV Shows': want 'tv shows', got %q", item2.SortName)
	}
}

func TestCollectionFolderItem_NullableArraysInitialized(t *testing.T) {
	srv := newTestServer()
	item := srv.newCollectionFolderItem("Movies", viewMoviesID, "movies", map[string]string{})

	b, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)

	for _, f := range []string{"ExternalUrls", "Taglines", "Genres", "RemoteTrailers", "Tags", "LockedFields", "BackdropImageTags"} {
		v, ok := m[f]
		if !ok {
			t.Errorf("field %q missing", f)
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			t.Errorf("field %q: want [], got %T", f, v)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("field %q: want empty [], got len=%d", f, len(arr))
		}
	}

	for _, f := range []string{"ProviderIds", "ImageTags", "ImageBlurHashes"} {
		v, ok := m[f]
		if !ok {
			t.Errorf("field %q missing", f)
			continue
		}
		if _, ok := v.(map[string]any); !ok {
			t.Errorf("field %q: want {}, got %T", f, v)
		}
	}
}

func TestRootFolderID_Is32CharHex(t *testing.T) {
	srv := newTestServer()
	id := srv.rootFolderID()
	assertHex32(t, "rootFolderID", id)
}

func TestRootFolderID_Deterministic(t *testing.T) {
	srv := newTestServer()
	id1 := srv.rootFolderID()
	id2 := srv.rootFolderID()
	if id1 != id2 {
		t.Errorf("rootFolderID not deterministic: %q vs %q", id1, id2)
	}
}
