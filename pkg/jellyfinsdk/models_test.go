package jellyfinsdk

import (
	"encoding/json"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestSDKBaseItemDtoRequiredFields(t *testing.T) {
	item := SDKBaseItemDto{
		ID:        "abc123",
		Type:      BaseItemKindMovie,
		MediaType: MediaTypeVideo,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Id"] != "abc123" {
		t.Errorf("Id: got %v, want abc123", raw["Id"])
	}
	if raw["Type"] != "Movie" {
		t.Errorf("Type: got %v, want Movie", raw["Type"])
	}
	if raw["MediaType"] != "Video" {
		t.Errorf("MediaType: got %v, want Video", raw["MediaType"])
	}
}

func TestSDKBaseItemDtoNullableFieldsOmitted(t *testing.T) {
	item := SDKBaseItemDto{
		ID:        "abc123",
		Type:      BaseItemKindMovie,
		MediaType: MediaTypeVideo,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	omittedFields := []string{
		"Name", "ServerId", "Overview", "ChannelId", "ChannelName",
		"Path", "Container", "RunTimeTicks", "ProductionYear",
		"IsFolder", "ParentId", "CollectionType", "CurrentProgram",
	}
	for _, field := range omittedFields {
		if _, exists := raw[field]; exists {
			t.Errorf("nullable field %q should be omitted when nil, but was present", field)
		}
	}
}

func TestSDKBaseItemDtoNullableFieldsPresent(t *testing.T) {
	item := SDKBaseItemDto{
		ID:             "abc123",
		Type:           BaseItemKindMovie,
		MediaType:      MediaTypeVideo,
		Name:           ptr("Test Movie"),
		Overview:       ptr("A test movie"),
		CommunityRating: ptr(float32(7.5)),
		RunTimeTicks:   ptr(int64(36000000000)),
		ProductionYear: ptr(2024),
		IsFolder:       ptr(false),
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Name"] != "Test Movie" {
		t.Errorf("Name: got %v, want Test Movie", raw["Name"])
	}
	if raw["Overview"] != "A test movie" {
		t.Errorf("Overview: got %v, want A test movie", raw["Overview"])
	}
	if raw["IsFolder"] != false {
		t.Errorf("IsFolder: got %v, want false", raw["IsFolder"])
	}
}

func TestSDKBaseItemDtoQueryResultNonNullableList(t *testing.T) {
	result := SDKBaseItemDtoQueryResult{
		Items:            []SDKBaseItemDto{},
		TotalRecordCount: 0,
		StartIndex:       0,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	items, ok := raw["Items"]
	if !ok {
		t.Fatal("Items field must be present (non-nullable)")
	}
	arr, ok := items.([]any)
	if !ok {
		t.Fatal("Items must be an array")
	}
	if len(arr) != 0 {
		t.Errorf("Items length: got %d, want 0", len(arr))
	}

	if raw["TotalRecordCount"] != float64(0) {
		t.Errorf("TotalRecordCount: got %v, want 0", raw["TotalRecordCount"])
	}
	if raw["StartIndex"] != float64(0) {
		t.Errorf("StartIndex: got %v, want 0", raw["StartIndex"])
	}
}

func TestSDKBaseItemDtoQueryResultNilItemsSerializesAsNull(t *testing.T) {
	result := SDKBaseItemDtoQueryResult{
		TotalRecordCount: 5,
		StartIndex:       0,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Items"] != nil {
		t.Errorf("nil Items slice should serialize as null, got %v", raw["Items"])
	}
}

func TestSDKUserDtoRequiredBooleans(t *testing.T) {
	user := SDKUserDto{
		ID:                        "user-1",
		HasPassword:               true,
		HasConfiguredPassword:     true,
		HasConfiguredEasyPassword: false,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["HasPassword"] != true {
		t.Errorf("HasPassword: got %v, want true", raw["HasPassword"])
	}
	if raw["HasConfiguredPassword"] != true {
		t.Errorf("HasConfiguredPassword: got %v, want true", raw["HasConfiguredPassword"])
	}
	if raw["HasConfiguredEasyPassword"] != false {
		t.Errorf("HasConfiguredEasyPassword: got %v, want false", raw["HasConfiguredEasyPassword"])
	}
}

func TestSDKUserDtoNullableFieldsOmitted(t *testing.T) {
	user := SDKUserDto{
		ID:                        "user-1",
		HasPassword:               true,
		HasConfiguredPassword:     true,
		HasConfiguredEasyPassword: false,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	omittedFields := []string{
		"Name", "ServerId", "ServerName", "PrimaryImageTag",
		"EnableAutoLogin", "LastLoginDate", "LastActivityDate",
		"Configuration", "Policy", "PrimaryImageAspectRatio",
	}
	for _, field := range omittedFields {
		if _, exists := raw[field]; exists {
			t.Errorf("nullable field %q should be omitted when nil", field)
		}
	}
}

func TestSDKAuthenticationResultAllNullable(t *testing.T) {
	result := SDKAuthenticationResult{}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if len(raw) != 0 {
		t.Errorf("empty AuthenticationResult should have no fields, got %d: %v", len(raw), raw)
	}
}

func TestSDKAuthenticationResultPopulated(t *testing.T) {
	result := SDKAuthenticationResult{
		AccessToken: ptr("test-token"),
		ServerID:    ptr("server-1"),
		User: &SDKUserDto{
			ID:                    "user-1",
			HasPassword:           true,
			HasConfiguredPassword: true,
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["AccessToken"] != "test-token" {
		t.Errorf("AccessToken: got %v, want test-token", raw["AccessToken"])
	}
	if raw["ServerId"] != "server-1" {
		t.Errorf("ServerId: got %v, want server-1", raw["ServerId"])
	}
	if raw["User"] == nil {
		t.Error("User should be present")
	}
}

func TestSDKSessionInfoDtoRequiredFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	session := SDKSessionInfoDto{
		PlayableMediaTypes:    []string{"Video", "Audio"},
		UserID:                "user-1",
		LastActivityDate:      now,
		LastPlaybackCheckIn:   now,
		IsActive:              true,
		SupportsMediaControl:  true,
		SupportsRemoteControl: false,
		HasCustomDeviceName:   false,
		SupportedCommands:     []string{},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["UserId"] != "user-1" {
		t.Errorf("UserId: got %v, want user-1", raw["UserId"])
	}
	if raw["IsActive"] != true {
		t.Errorf("IsActive: got %v, want true", raw["IsActive"])
	}
	if raw["SupportsMediaControl"] != true {
		t.Errorf("SupportsMediaControl: got %v, want true", raw["SupportsMediaControl"])
	}
	if raw["HasCustomDeviceName"] != false {
		t.Errorf("HasCustomDeviceName: got %v, want false", raw["HasCustomDeviceName"])
	}

	mediaTypes, ok := raw["PlayableMediaTypes"].([]any)
	if !ok {
		t.Fatal("PlayableMediaTypes must be an array")
	}
	if len(mediaTypes) != 2 {
		t.Errorf("PlayableMediaTypes length: got %d, want 2", len(mediaTypes))
	}
}

func TestSDKPlayerStateInfoRequiredFields(t *testing.T) {
	state := SDKPlayerStateInfo{
		CanSeek:       true,
		IsPaused:      false,
		IsMuted:       false,
		RepeatMode:    "RepeatNone",
		PlaybackOrder: "Default",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["CanSeek"] != true {
		t.Errorf("CanSeek: got %v, want true", raw["CanSeek"])
	}
	if raw["RepeatMode"] != "RepeatNone" {
		t.Errorf("RepeatMode: got %v, want RepeatNone", raw["RepeatMode"])
	}
	if raw["PlaybackOrder"] != "Default" {
		t.Errorf("PlaybackOrder: got %v, want Default", raw["PlaybackOrder"])
	}

	if _, exists := raw["PositionTicks"]; exists {
		t.Error("PositionTicks should be omitted when nil")
	}
}

func TestSDKMediaStreamRequiredFields(t *testing.T) {
	stream := SDKMediaStream{
		Type:                 MediaStreamTypeVideo,
		Index:                0,
		IsDefault:            true,
		IsForced:             false,
		IsHearingImpaired:    false,
		IsExternal:           false,
		IsTextSubtitleStream: false,
		SupportsExternalStream: true,
		IsInterlaced:         false,
		VideoRange:           VideoRangeSDR,
		VideoRangeType:       VideoRangeTypeSDR,
		AudioSpatialFormat:   "None",
	}

	data, err := json.Marshal(stream)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Type"] != "Video" {
		t.Errorf("Type: got %v, want Video", raw["Type"])
	}
	if raw["Index"] != float64(0) {
		t.Errorf("Index: got %v, want 0", raw["Index"])
	}
	if raw["IsDefault"] != true {
		t.Errorf("IsDefault: got %v, want true", raw["IsDefault"])
	}
	if raw["VideoRange"] != "SDR" {
		t.Errorf("VideoRange: got %v, want SDR", raw["VideoRange"])
	}
	if raw["AudioSpatialFormat"] != "None" {
		t.Errorf("AudioSpatialFormat: got %v, want None", raw["AudioSpatialFormat"])
	}

	if _, exists := raw["Codec"]; exists {
		t.Error("Codec should be omitted when nil")
	}
	if _, exists := raw["Width"]; exists {
		t.Error("Width should be omitted when nil")
	}
}

func TestSDKMediaSourceInfoRequiredBooleans(t *testing.T) {
	source := SDKMediaSourceInfo{
		Protocol:               MediaProtocolHTTP,
		Type:                   MediaSourceTypeDefault,
		IsRemote:               true,
		SupportsTranscoding:    true,
		SupportsDirectStream:   true,
		SupportsDirectPlay:     false,
		IsInfiniteStream:       true,
		TranscodingSubProtocol: "hls",
	}

	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Protocol"] != "Http" {
		t.Errorf("Protocol: got %v, want Http", raw["Protocol"])
	}
	if raw["Type"] != "Default" {
		t.Errorf("Type: got %v, want Default", raw["Type"])
	}
	if raw["IsRemote"] != true {
		t.Errorf("IsRemote: got %v, want true", raw["IsRemote"])
	}
	if raw["SupportsDirectPlay"] != false {
		t.Errorf("SupportsDirectPlay: got %v, want false", raw["SupportsDirectPlay"])
	}
	if raw["IsInfiniteStream"] != true {
		t.Errorf("IsInfiniteStream: got %v, want true", raw["IsInfiniteStream"])
	}
	if raw["TranscodingSubProtocol"] != "hls" {
		t.Errorf("TranscodingSubProtocol: got %v, want hls", raw["TranscodingSubProtocol"])
	}

	boolFields := []string{
		"ReadAtNativeFramerate", "IgnoreDts", "IgnoreIndex", "GenPtsInput",
		"RequiresOpening", "RequiresClosing", "RequiresLooping", "SupportsProbing",
		"HasSegments", "UseMostCompatibleTranscodingProfile",
	}
	for _, field := range boolFields {
		val, exists := raw[field]
		if !exists {
			t.Errorf("non-nullable bool %q must be present", field)
		}
		if val != false {
			t.Errorf("non-nullable bool %q default: got %v, want false", field, val)
		}
	}
}

func TestSDKDisplayPreferencesDtoRequiredFields(t *testing.T) {
	prefs := SDKDisplayPreferencesDto{
		RememberIndexing:   false,
		PrimaryImageHeight: 300,
		PrimaryImageWidth:  200,
		CustomPrefs:        map[string]*string{},
		ScrollDirection:    ScrollDirectionVertical,
		ShowBackdrop:       true,
		RememberSorting:    false,
		SortOrder:          SortOrderAscending,
		ShowSidebar:        true,
	}

	data, err := json.Marshal(prefs)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["PrimaryImageHeight"] != float64(300) {
		t.Errorf("PrimaryImageHeight: got %v, want 300", raw["PrimaryImageHeight"])
	}
	if raw["ScrollDirection"] != "Vertical" {
		t.Errorf("ScrollDirection: got %v, want Vertical", raw["ScrollDirection"])
	}
	if raw["SortOrder"] != "Ascending" {
		t.Errorf("SortOrder: got %v, want Ascending", raw["SortOrder"])
	}
	if raw["ShowBackdrop"] != true {
		t.Errorf("ShowBackdrop: got %v, want true", raw["ShowBackdrop"])
	}
}

func TestSDKPublicSystemInfoAllNullable(t *testing.T) {
	info := SDKPublicSystemInfo{}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if len(raw) != 0 {
		t.Errorf("empty PublicSystemInfo should have no fields, got %d: %v", len(raw), raw)
	}
}

func TestSDKPublicSystemInfoPopulated(t *testing.T) {
	info := SDKPublicSystemInfo{
		ServerName:             ptr("TVProxy"),
		Version:                ptr("10.10.0"),
		ProductName:            ptr("Jellyfin Server"),
		ID:                     ptr("abc-123"),
		StartupWizardCompleted: ptr(true),
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["ServerName"] != "TVProxy" {
		t.Errorf("ServerName: got %v, want TVProxy", raw["ServerName"])
	}
	if raw["Version"] != "10.10.0" {
		t.Errorf("Version: got %v, want 10.10.0", raw["Version"])
	}
	if raw["StartupWizardCompleted"] != true {
		t.Errorf("StartupWizardCompleted: got %v, want true", raw["StartupWizardCompleted"])
	}
}

func TestSDKUserItemDataDtoRequiredFields(t *testing.T) {
	data := SDKUserItemDataDto{
		PlaybackPositionTicks: 0,
		PlayCount:             0,
		IsFavorite:            false,
		Played:                false,
		Key:                   "item-key",
		ItemID:                "item-id",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		t.Fatal(err)
	}

	requiredFields := []string{
		"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "Key", "ItemId",
	}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("required field %q must be present", field)
		}
	}

	if _, exists := raw["Rating"]; exists {
		t.Error("Rating should be omitted when nil")
	}
	if _, exists := raw["PlayedPercentage"]; exists {
		t.Error("PlayedPercentage should be omitted when nil")
	}
}

func TestSDKBaseItemPersonFieldNames(t *testing.T) {
	person := SDKBaseItemPerson{
		Name: ptr("John Doe"),
		ID:   "person-1",
		Role: ptr("Tony Stark"),
		Type: PersonKindActor,
	}

	data, err := json.Marshal(person)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["Name"] != "John Doe" {
		t.Errorf("Name: got %v, want John Doe", raw["Name"])
	}
	if raw["Id"] != "person-1" {
		t.Errorf("Id: got %v, want person-1", raw["Id"])
	}
	if raw["Role"] != "Tony Stark" {
		t.Errorf("Role: got %v, want Tony Stark", raw["Role"])
	}
	if raw["Type"] != "Actor" {
		t.Errorf("Type: got %v, want Actor", raw["Type"])
	}
}

func TestSDKBaseItemDtoImageTags(t *testing.T) {
	item := SDKBaseItemDto{
		ID:        "abc123",
		Type:      BaseItemKindMovie,
		MediaType: MediaTypeVideo,
		ImageTags: map[SDKImageType]string{
			ImageTypePrimary:  "tag1",
			ImageTypeBackdrop: "tag2",
		},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	tags, ok := raw["ImageTags"].(map[string]any)
	if !ok {
		t.Fatal("ImageTags must be a map")
	}
	if tags["Primary"] != "tag1" {
		t.Errorf("ImageTags.Primary: got %v, want tag1", tags["Primary"])
	}
	if tags["Backdrop"] != "tag2" {
		t.Errorf("ImageTags.Backdrop: got %v, want tag2", tags["Backdrop"])
	}
}

func TestSDKClientCapabilitiesDtoNonNullableLists(t *testing.T) {
	caps := SDKClientCapabilitiesDto{
		PlayableMediaTypes:          []string{},
		SupportedCommands:           []string{},
		SupportsMediaControl:        false,
		SupportsPersistentIdentifier: true,
	}

	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	mediaTypes, ok := raw["PlayableMediaTypes"].([]any)
	if !ok {
		t.Fatal("PlayableMediaTypes must be present as array")
	}
	if len(mediaTypes) != 0 {
		t.Errorf("PlayableMediaTypes: got %d elements, want 0", len(mediaTypes))
	}

	commands, ok := raw["SupportedCommands"].([]any)
	if !ok {
		t.Fatal("SupportedCommands must be present as array")
	}
	if len(commands) != 0 {
		t.Errorf("SupportedCommands: got %d elements, want 0", len(commands))
	}
}

func TestSDKRoundTripDeserialization(t *testing.T) {
	original := SDKBaseItemDto{
		ID:             "round-trip",
		Type:           BaseItemKindLiveTvChannel,
		MediaType:      MediaTypeVideo,
		Name:           ptr("BBC One"),
		ChannelNumber:  ptr("101"),
		IsFolder:       ptr(false),
		RunTimeTicks:   ptr(int64(36000000000)),
		CommunityRating: ptr(float32(8.5)),
		ImageTags: map[SDKImageType]string{
			ImageTypePrimary: "abc",
		},
		UserData: &SDKUserItemDataDto{
			PlaybackPositionTicks: 100,
			PlayCount:             1,
			IsFavorite:            true,
			Played:                false,
			Key:                   "key1",
			ItemID:                "round-trip",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SDKBaseItemDto
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %v, want %v", decoded.ID, original.ID)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type: got %v, want %v", decoded.Type, original.Type)
	}
	if *decoded.Name != *original.Name {
		t.Errorf("Name: got %v, want %v", *decoded.Name, *original.Name)
	}
	if *decoded.ChannelNumber != *original.ChannelNumber {
		t.Errorf("ChannelNumber: got %v, want %v", *decoded.ChannelNumber, *original.ChannelNumber)
	}
	if decoded.UserData == nil {
		t.Fatal("UserData should not be nil")
	}
	if decoded.UserData.IsFavorite != true {
		t.Errorf("UserData.IsFavorite: got %v, want true", decoded.UserData.IsFavorite)
	}
}
