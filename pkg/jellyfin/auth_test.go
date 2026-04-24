package jellyfin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJellyfinTime_MarshalJSON_SevenDecimalPlaces(t *testing.T) {
	ts := JellyfinTime{time.Date(2026, 4, 24, 19, 26, 6, 988716100, time.UTC)}
	b, err := ts.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasSuffix(s, `Z"`) {
		t.Errorf("expected Z suffix, got %s", s)
	}
	inner := s[1 : len(s)-1]
	dotIdx := strings.Index(inner, ".")
	if dotIdx < 0 {
		t.Fatalf("no decimal point in %s", inner)
	}
	fracPart := inner[dotIdx+1 : len(inner)-1]
	if len(fracPart) != 7 {
		t.Errorf("expected 7 decimal places, got %d in %s", len(fracPart), inner)
	}
}

func TestJellyfinTime_MarshalJSON_ZeroTime(t *testing.T) {
	ts := JellyfinTime{time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)}
	b, err := ts.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `"0001-01-01T00:00:00.0000000Z"`
	if string(b) != want {
		t.Errorf("got %s want %s", string(b), want)
	}
}

func TestJellyfinTime_UnmarshalJSON_RoundTrip(t *testing.T) {
	original := `"2026-04-24T19:26:06.9887161Z"`
	var jt JellyfinTime
	if err := json.Unmarshal([]byte(original), &jt); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(jt)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original {
		t.Errorf("round trip failed: got %s want %s", string(b), original)
	}
}

func TestDefaultUserConfig_AllFields(t *testing.T) {
	cfg := defaultUserConfig()

	if !cfg.PlayDefaultAudioTrack {
		t.Error("PlayDefaultAudioTrack should be true")
	}
	if cfg.SubtitleMode != "Default" {
		t.Errorf("SubtitleMode: got %s want Default", cfg.SubtitleMode)
	}
	if cfg.GroupedFolders == nil {
		t.Error("GroupedFolders must not be nil")
	}
	if cfg.OrderedViews == nil {
		t.Error("OrderedViews must not be nil")
	}
	if cfg.LatestItemsExcludes == nil {
		t.Error("LatestItemsExcludes must not be nil")
	}
	if cfg.MyMediaExcludes == nil {
		t.Error("MyMediaExcludes must not be nil")
	}
	if !cfg.HidePlayedInLatest {
		t.Error("HidePlayedInLatest should be true")
	}
	if !cfg.RememberAudioSelections {
		t.Error("RememberAudioSelections should be true")
	}
	if !cfg.RememberSubtitleSelections {
		t.Error("RememberSubtitleSelections should be true")
	}
	if !cfg.EnableNextEpisodeAutoPlay {
		t.Error("EnableNextEpisodeAutoPlay should be true")
	}
	if cfg.CastReceiverId != "F007D354" {
		t.Errorf("CastReceiverId: got %s want F007D354", cfg.CastReceiverId)
	}
}

func TestDefaultUserConfig_JSONShape(t *testing.T) {
	cfg := defaultUserConfig()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}

	requiredKeys := []string{
		"PlayDefaultAudioTrack", "SubtitleLanguagePreference", "DisplayMissingEpisodes",
		"GroupedFolders", "SubtitleMode", "DisplayCollectionsView", "EnableLocalPassword",
		"OrderedViews", "LatestItemsExcludes", "MyMediaExcludes", "HidePlayedInLatest",
		"RememberAudioSelections", "RememberSubtitleSelections", "EnableNextEpisodeAutoPlay",
		"CastReceiverId",
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %s in UserConfig JSON", k)
		}
	}

	for _, arrKey := range []string{"GroupedFolders", "OrderedViews", "LatestItemsExcludes", "MyMediaExcludes"} {
		v, ok := m[arrKey]
		if !ok {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			t.Errorf("%s should be array, got %T", arrKey, v)
			continue
		}
		if len(arr) != 0 {
			t.Errorf("%s should be empty array", arrKey)
		}
	}
}

func TestDefaultPolicy_AdminFields(t *testing.T) {
	p := defaultPolicy(true)
	if !p.IsAdministrator {
		t.Error("admin: IsAdministrator should be true")
	}
	if !p.IsHidden {
		t.Error("admin: IsHidden should be true")
	}
	if !p.EnableRemoteControlOfOtherUsers {
		t.Error("admin: EnableRemoteControlOfOtherUsers should be true")
	}
	if !p.EnableLiveTvManagement {
		t.Error("admin: EnableLiveTvManagement should be true")
	}
	if !p.EnableContentDeletion {
		t.Error("admin: EnableContentDeletion should be true")
	}
}

func TestDefaultPolicy_NonAdminFields(t *testing.T) {
	p := defaultPolicy(false)
	if p.IsAdministrator {
		t.Error("non-admin: IsAdministrator should be false")
	}
	if p.IsHidden {
		t.Error("non-admin: IsHidden should be false")
	}
	if p.EnableRemoteControlOfOtherUsers {
		t.Error("non-admin: EnableRemoteControlOfOtherUsers should be false")
	}
	if p.EnableLiveTvManagement {
		t.Error("non-admin: EnableLiveTvManagement should be false")
	}
	if p.EnableContentDeletion {
		t.Error("non-admin: EnableContentDeletion should be false")
	}
}

func TestDefaultPolicy_JSONShape(t *testing.T) {
	p := defaultPolicy(true)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}

	requiredKeys := []string{
		"IsAdministrator", "IsHidden", "EnableCollectionManagement", "EnableSubtitleManagement",
		"EnableLyricManagement", "IsDisabled", "BlockedTags", "AllowedTags",
		"EnableUserPreferenceAccess", "AccessSchedules", "BlockUnratedItems",
		"EnableRemoteControlOfOtherUsers", "EnableSharedDeviceControl", "EnableRemoteAccess",
		"EnableLiveTvManagement", "EnableLiveTvAccess", "EnableMediaPlayback",
		"EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding", "EnablePlaybackRemuxing",
		"ForceRemoteSourceTranscoding", "EnableContentDeletion", "EnableContentDeletionFromFolders",
		"EnableContentDownloading", "EnableSyncTranscoding", "EnableMediaConversion",
		"EnabledDevices", "EnableAllDevices", "EnabledChannels", "EnableAllChannels",
		"EnabledFolders", "EnableAllFolders", "InvalidLoginAttemptCount",
		"LoginAttemptsBeforeLockout", "MaxActiveSessions", "EnablePublicSharing",
		"BlockedMediaFolders", "BlockedChannels", "RemoteClientBitrateLimit",
		"AuthenticationProviderId", "PasswordResetProviderId", "SyncPlayAccess",
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %s in UserPolicy JSON", k)
		}
	}

	arrayFields := []string{
		"BlockedTags", "AllowedTags", "BlockUnratedItems",
		"EnableContentDeletionFromFolders", "EnabledDevices", "EnabledChannels",
		"EnabledFolders", "BlockedMediaFolders", "BlockedChannels",
	}
	for _, k := range arrayFields {
		v, ok := m[k]
		if !ok {
			continue
		}
		if _, ok := v.([]any); !ok {
			t.Errorf("%s must be array, got %T", k, v)
		}
	}

	if m["AuthenticationProviderId"] != "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider" {
		t.Errorf("unexpected AuthenticationProviderId: %v", m["AuthenticationProviderId"])
	}
	if m["PasswordResetProviderId"] != "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider" {
		t.Errorf("unexpected PasswordResetProviderId: %v", m["PasswordResetProviderId"])
	}
	if m["SyncPlayAccess"] != "CreateAndJoinGroups" {
		t.Errorf("unexpected SyncPlayAccess: %v", m["SyncPlayAccess"])
	}
	if m["LoginAttemptsBeforeLockout"] != float64(-1) {
		t.Errorf("LoginAttemptsBeforeLockout should be -1, got %v", m["LoginAttemptsBeforeLockout"])
	}
}

func TestJellyfinPlayableMediaTypes(t *testing.T) {
	types := jellyfinPlayableMediaTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 media types, got %d", len(types))
	}
	if types[0] != "Audio" || types[1] != "Video" {
		t.Errorf("expected [Audio Video], got %v", types)
	}
}

func TestJellyfinSupportedCommands_ContainsRequired(t *testing.T) {
	cmds := jellyfinSupportedCommands()
	required := []string{
		"MoveUp", "MoveDown", "MoveLeft", "MoveRight",
		"Select", "Back", "GoHome", "GoToSettings",
		"VolumeUp", "VolumeDown", "Mute", "Unmute", "ToggleMute",
		"SetVolume", "SetAudioStreamIndex", "SetSubtitleStreamIndex",
		"ChannelUp", "ChannelDown", "PlayMediaSource", "PlayTrailers",
		"SetRepeatMode", "SetShuffleQueue",
	}
	cmdSet := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		cmdSet[c] = true
	}
	for _, r := range required {
		if !cmdSet[r] {
			t.Errorf("missing required command: %s", r)
		}
	}
}

func TestAuthenticationResult_JSONShape(t *testing.T) {
	nowJF := JellyfinTime{time.Now().UTC()}
	result := AuthenticationResult{
		User: &UserDto{
			Name:                  "admin",
			ServerID:              "61da34e701b54548a25c783de5e13284",
			ID:                    "d39ff01f781e46788ca024a8a59c2a19",
			HasPassword:           true,
			HasConfiguredPassword: true,
			LastLoginDate:         &nowJF,
			LastActivityDate:      &nowJF,
			Configuration:         defaultUserConfig(),
			Policy:                defaultPolicy(true),
		},
		SessionInfo: &SessionInfo{
			PlayState:       &PlayState{RepeatMode: "RepeatNone", PlaybackOrder: "Default"},
			AdditionalUsers: []any{},
			Capabilities: &SessionCapabilities{
				PlayableMediaTypes:           jellyfinPlayableMediaTypes(),
				SupportedCommands:            jellyfinSupportedCommands(),
				SupportsMediaControl:         true,
				SupportsPersistentIdentifier: false,
			},
			RemoteEndPoint:           "10.0.0.2",
			PlayableMediaTypes:       jellyfinPlayableMediaTypes(),
			ID:                       "6752724f1292df5eaaeb479f1867c321",
			UserID:                   "d39ff01f781e46788ca024a8a59c2a19",
			UserName:                 "admin",
			Client:                   "Jellyfin Media Player",
			LastActivityDate:         nowJF,
			LastPlaybackCheckIn:      "0001-01-01T00:00:00.0000000Z",
			DeviceName:               "Test Device",
			DeviceID:                 "testdeviceid",
			ApplicationVersion:       "1.12.0",
			IsActive:                 true,
			NowPlayingQueue:          []any{},
			NowPlayingQueueFullItems: []any{},
			ServerID:                 "61da34e701b54548a25c783de5e13284",
			SupportedCommands:        jellyfinSupportedCommands(),
		},
		AccessToken: "9c909f32dbd24537b498c1b1dab30a5e",
		ServerID:    "61da34e701b54548a25c783de5e13284",
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}

	if _, ok := m["User"]; !ok {
		t.Error("missing User")
	}
	if _, ok := m["SessionInfo"]; !ok {
		t.Error("missing SessionInfo")
	}
	if _, ok := m["AccessToken"]; !ok {
		t.Error("missing AccessToken")
	}
	if _, ok := m["ServerId"]; !ok {
		t.Error("missing ServerId")
	}

	user, ok := m["User"].(map[string]any)
	if !ok {
		t.Fatal("User is not an object")
	}
	if _, ok := user["ServerName"]; ok {
		t.Error("User must NOT include ServerName")
	}
	if user["ServerId"] == "" {
		t.Error("User.ServerId must not be empty")
	}

	cfg, ok := user["Configuration"].(map[string]any)
	if !ok {
		t.Fatal("User.Configuration is not an object")
	}
	if cfg["CastReceiverId"] != "F007D354" {
		t.Errorf("CastReceiverId: got %v want F007D354", cfg["CastReceiverId"])
	}

	session, ok := m["SessionInfo"].(map[string]any)
	if !ok {
		t.Fatal("SessionInfo is not an object")
	}
	if session["AdditionalUsers"] == nil {
		t.Error("AdditionalUsers must be present (even if empty)")
	}
	if session["NowPlayingQueue"] == nil {
		t.Error("NowPlayingQueue must be present")
	}
	if session["NowPlayingQueueFullItems"] == nil {
		t.Error("NowPlayingQueueFullItems must be present")
	}

	caps, ok := session["Capabilities"].(map[string]any)
	if !ok {
		t.Fatal("Capabilities is not an object")
	}
	pmt, ok := caps["PlayableMediaTypes"].([]any)
	if !ok || len(pmt) == 0 {
		t.Error("Capabilities.PlayableMediaTypes must be non-empty array")
	}
	sc, ok := caps["SupportedCommands"].([]any)
	if !ok || len(sc) == 0 {
		t.Error("Capabilities.SupportedCommands must be non-empty array")
	}

	sessionPMT, ok := session["PlayableMediaTypes"].([]any)
	if !ok || len(sessionPMT) == 0 {
		t.Error("SessionInfo.PlayableMediaTypes must be non-empty array")
	}

	sessionSC, ok := session["SupportedCommands"].([]any)
	if !ok || len(sessionSC) == 0 {
		t.Error("SessionInfo.SupportedCommands must be non-empty array")
	}

	if session["LastPlaybackCheckIn"] != "0001-01-01T00:00:00.0000000Z" {
		t.Errorf("LastPlaybackCheckIn: got %v", session["LastPlaybackCheckIn"])
	}
}

func TestUserDto_TimestampFormat(t *testing.T) {
	nowJF := JellyfinTime{time.Now().UTC()}
	u := UserDto{
		Name:             "test",
		ServerID:         "abc123",
		ID:               "def456",
		LastLoginDate:    &nowJF,
		LastActivityDate: &nowJF,
		Configuration:    defaultUserConfig(),
		Policy:           defaultPolicy(false),
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)

	for _, field := range []string{"LastLoginDate", "LastActivityDate"} {
		v, ok := m[field].(string)
		if !ok {
			t.Errorf("%s should be string", field)
			continue
		}
		if !strings.HasSuffix(v, "Z") {
			t.Errorf("%s should end with Z, got %s", field, v)
		}
		dotIdx := strings.Index(v, ".")
		if dotIdx < 0 {
			t.Errorf("%s has no decimal point: %s", field, v)
			continue
		}
		fracPart := v[dotIdx+1 : len(v)-1]
		if len(fracPart) != 7 {
			t.Errorf("%s should have 7 decimal places, got %d in %s", field, len(fracPart), v)
		}
	}
}
