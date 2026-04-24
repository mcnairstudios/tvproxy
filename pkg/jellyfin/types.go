package jellyfin

import (
	"fmt"
	"time"
)

type JellyfinTime struct {
	time.Time
}

func (jt JellyfinTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + jt.UTC().Format("2006-01-02T15:04:05.0000000Z") + `"`), nil
}

func (jt *JellyfinTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) < 2 {
		return fmt.Errorf("invalid time: %s", data)
	}
	s := string(data[1 : len(data)-1])
	formats := []string{
		"2006-01-02T15:04:05.0000000Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			jt.Time = t
			return nil
		}
	}
	return fmt.Errorf("cannot parse time: %s", s)
}

type PublicSystemInfo struct {
	LocalAddress           string `json:"LocalAddress"`
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	OperatingSystem        string `json:"OperatingSystem"`
	ID                     string `json:"Id"`
	StartupWizardCompleted bool   `json:"StartupWizardCompleted"`
}

type SystemInfo struct {
	PublicSystemInfo
	OperatingSystemDisplayName string `json:"OperatingSystemDisplayName"`
	HasPendingRestart          bool   `json:"HasPendingRestart"`
	IsShuttingDown             bool   `json:"IsShuttingDown"`
	SupportsLibraryMonitor     bool   `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber        int    `json:"WebSocketPortNumber"`
	CanSelfRestart             bool   `json:"CanSelfRestart"`
	CanLaunchWebBrowser        bool   `json:"CanLaunchWebBrowser"`
	HasUpdateAvailable         bool   `json:"HasUpdateAvailable"`
}

type AuthenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"`
}

type AuthenticationResult struct {
	User        *UserDto     `json:"User"`
	SessionInfo *SessionInfo `json:"SessionInfo"`
	AccessToken string       `json:"AccessToken"`
	ServerID    string       `json:"ServerId"`
}

type UserDto struct {
	Name                      string         `json:"Name"`
	ServerID                  string         `json:"ServerId"`
	ID                        string         `json:"Id"`
	PrimaryImageTag           string         `json:"PrimaryImageTag,omitempty"`
	HasPassword               bool           `json:"HasPassword"`
	HasConfiguredPassword     bool           `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool           `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           bool           `json:"EnableAutoLogin"`
	LastLoginDate             *JellyfinTime  `json:"LastLoginDate,omitempty"`
	LastActivityDate          *JellyfinTime  `json:"LastActivityDate,omitempty"`
	Configuration             UserConfig     `json:"Configuration"`
	Policy                    UserPolicy     `json:"Policy"`
}

type UserConfig struct {
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string   `json:"SubtitleLanguagePreference"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	GroupedFolders             []string `json:"GroupedFolders"`
	SubtitleMode               string   `json:"SubtitleMode"`
	DisplayCollectionsView     bool     `json:"DisplayCollectionsView"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	OrderedViews               []string `json:"OrderedViews"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
	CastReceiverId             string   `json:"CastReceiverId"`
}

type UserPolicy struct {
	IsAdministrator                 bool     `json:"IsAdministrator"`
	IsHidden                        bool     `json:"IsHidden"`
	EnableCollectionManagement      bool     `json:"EnableCollectionManagement"`
	EnableSubtitleManagement        bool     `json:"EnableSubtitleManagement"`
	EnableLyricManagement           bool     `json:"EnableLyricManagement"`
	IsDisabled                      bool     `json:"IsDisabled"`
	BlockedTags                     []string `json:"BlockedTags"`
	AllowedTags                     []string `json:"AllowedTags"`
	EnableUserPreferenceAccess      bool     `json:"EnableUserPreferenceAccess"`
	AccessSchedules                 []any    `json:"AccessSchedules"`
	BlockUnratedItems               []string `json:"BlockUnratedItems"`
	EnableRemoteControlOfOtherUsers bool     `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl       bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess              bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement          bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess              bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback             bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding  bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding  bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing          bool     `json:"EnablePlaybackRemuxing"`
	ForceRemoteSourceTranscoding    bool     `json:"ForceRemoteSourceTranscoding"`
	EnableContentDeletion           bool     `json:"EnableContentDeletion"`
	EnableContentDeletionFromFolders []string `json:"EnableContentDeletionFromFolders"`
	EnableContentDownloading        bool     `json:"EnableContentDownloading"`
	EnableSyncTranscoding           bool     `json:"EnableSyncTranscoding"`
	EnableMediaConversion           bool     `json:"EnableMediaConversion"`
	EnabledDevices                  []string `json:"EnabledDevices"`
	EnableAllDevices                bool     `json:"EnableAllDevices"`
	EnabledChannels                 []string `json:"EnabledChannels"`
	EnableAllChannels               bool     `json:"EnableAllChannels"`
	EnabledFolders                  []string `json:"EnabledFolders"`
	EnableAllFolders                bool     `json:"EnableAllFolders"`
	InvalidLoginAttemptCount        int      `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout      int      `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions               int      `json:"MaxActiveSessions"`
	EnablePublicSharing             bool     `json:"EnablePublicSharing"`
	BlockedMediaFolders             []string `json:"BlockedMediaFolders"`
	BlockedChannels                 []string `json:"BlockedChannels"`
	RemoteClientBitrateLimit        int      `json:"RemoteClientBitrateLimit"`
	AuthenticationProviderId        string   `json:"AuthenticationProviderId"`
	PasswordResetProviderId         string   `json:"PasswordResetProviderId"`
	SyncPlayAccess                  string   `json:"SyncPlayAccess"`
}

type SessionInfo struct {
	PlayState             *PlayState          `json:"PlayState,omitempty"`
	AdditionalUsers       []any               `json:"AdditionalUsers"`
	Capabilities          *SessionCapabilities `json:"Capabilities,omitempty"`
	RemoteEndPoint        string              `json:"RemoteEndPoint,omitempty"`
	PlayableMediaTypes    []string            `json:"PlayableMediaTypes"`
	ID                    string              `json:"Id"`
	UserID                string              `json:"UserId"`
	UserName              string              `json:"UserName"`
	Client                string              `json:"Client"`
	LastActivityDate      JellyfinTime        `json:"LastActivityDate"`
	LastPlaybackCheckIn   string              `json:"LastPlaybackCheckIn"`
	DeviceName            string              `json:"DeviceName"`
	DeviceID              string              `json:"DeviceId"`
	ApplicationVersion    string              `json:"ApplicationVersion"`
	IsActive              bool                `json:"IsActive"`
	SupportsMediaControl  bool                `json:"SupportsMediaControl"`
	SupportsRemoteControl bool                `json:"SupportsRemoteControl"`
	NowPlayingQueue       []any               `json:"NowPlayingQueue"`
	NowPlayingQueueFullItems []any            `json:"NowPlayingQueueFullItems"`
	HasCustomDeviceName   bool                `json:"HasCustomDeviceName"`
	ServerID              string              `json:"ServerId"`
	SupportedCommands     []string            `json:"SupportedCommands"`
}

type SessionCapabilities struct {
	PlayableMediaTypes       []string `json:"PlayableMediaTypes"`
	SupportedCommands        []string `json:"SupportedCommands"`
	SupportsMediaControl     bool     `json:"SupportsMediaControl"`
	SupportsPersistentIdentifier bool `json:"SupportsPersistentIdentifier"`
}

type PlayState struct {
	CanSeek       bool   `json:"CanSeek"`
	IsPaused      bool   `json:"IsPaused"`
	IsMuted       bool   `json:"IsMuted"`
	RepeatMode    string `json:"RepeatMode"`
	PlaybackOrder string `json:"PlaybackOrder"`
}

type BaseItemDto struct {
	Name                     string            `json:"Name"`
	ServerID                 string            `json:"ServerId"`
	ID                       string            `json:"Id"`
	Etag                     string            `json:"Etag,omitempty"`
	CanDelete                *bool             `json:"CanDelete,omitempty"`
	CanDownload              *bool             `json:"CanDownload,omitempty"`
	DateCreated              string            `json:"DateCreated,omitempty"`
	DateLastMediaAdded       string            `json:"DateLastMediaAdded,omitempty"`
	HasSubtitles             bool              `json:"HasSubtitles,omitempty"`
	Container                string            `json:"Container,omitempty"`
	SortName                 string            `json:"SortName,omitempty"`
	PremiereDate             string            `json:"PremiereDate,omitempty"`
	Path                     string            `json:"Path,omitempty"`
	EnableMediaSourceDisplay bool              `json:"EnableMediaSourceDisplay,omitempty"`
	OfficialRating           string            `json:"OfficialRating,omitempty"`
	Overview                 string            `json:"Overview,omitempty"`
	Genres                   []string          `json:"Genres,omitempty"`
	CommunityRating          float64           `json:"CommunityRating,omitempty"`
	RunTimeTicks             int64             `json:"RunTimeTicks,omitempty"`
	ProductionYear           int               `json:"ProductionYear,omitempty"`
	IsFolder                 bool              `json:"IsFolder"`
	Type                     string            `json:"Type"`
	CollectionType           string            `json:"CollectionType,omitempty"`
	ParentID                 string            `json:"ParentId,omitempty"`
	ChannelID                *string           `json:"ChannelId,omitempty"`
	VideoType                string            `json:"VideoType,omitempty"`
	PlayAccess               string            `json:"PlayAccess,omitempty"`
	IsHD                     bool              `json:"IsHD,omitempty"`
	Width                    int               `json:"Width,omitempty"`
	Height                   int               `json:"Height,omitempty"`
	ExternalUrls             []any             `json:"ExternalUrls,omitempty"`
	RemoteTrailers           []any             `json:"RemoteTrailers,omitempty"`
	ProviderIds              map[string]string `json:"ProviderIds,omitempty"`
	ProductionLocations      []string          `json:"ProductionLocations,omitempty"`
	Chapters                 []any             `json:"Chapters,omitempty"`
	Trickplay                map[string]any    `json:"Trickplay,omitempty"`
	ImageTags                map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags        []string          `json:"BackdropImageTags,omitempty"`
	ImageBlurHashes          map[string]any    `json:"ImageBlurHashes,omitempty"`
	LocationType             string            `json:"LocationType,omitempty"`
	MediaType                string            `json:"MediaType,omitempty"`
	ChildCount               int               `json:"ChildCount,omitempty"`
	SpecialFeatureCount      int               `json:"SpecialFeatureCount,omitempty"`
	LocalTrailerCount        int               `json:"LocalTrailerCount,omitempty"`
	DisplayPreferencesId     string            `json:"DisplayPreferencesId,omitempty"`
	Tags                     []string          `json:"Tags,omitempty"`
	LockedFields             []string          `json:"LockedFields,omitempty"`
	LockData                 *bool             `json:"LockData,omitempty"`
	SeriesName               string            `json:"SeriesName,omitempty"`
	SeriesID                 string            `json:"SeriesId,omitempty"`
	SeasonID                 string            `json:"SeasonId,omitempty"`
	IndexNumber              int               `json:"IndexNumber,omitempty"`
	ParentIndexNumber        int               `json:"ParentIndexNumber,omitempty"`
	UserData                 *UserItemData     `json:"UserData,omitempty"`
	MediaSources             []MediaSource     `json:"MediaSources,omitempty"`
	GenreItems               []NameIDPair      `json:"GenreItems,omitempty"`
	Taglines                 []string          `json:"Taglines,omitempty"`
	People                   []PersonDto       `json:"People,omitempty"`
	Studios                  []NameIDPair      `json:"Studios,omitempty"`
	ChannelNumber            string            `json:"ChannelNumber,omitempty"`
	ChannelPrimaryImageTag   string            `json:"ChannelPrimaryImageTag,omitempty"`
	CurrentProgram           *BaseItemDto      `json:"CurrentProgram,omitempty"`
}

type UserItemData struct {
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
	PlayCount             int    `json:"PlayCount"`
	IsFavorite            bool   `json:"IsFavorite"`
	Played                bool   `json:"Played"`
	Key                   string `json:"Key"`
	ItemID                string `json:"ItemId,omitempty"`
}

type MediaSource struct {
	Protocol                string        `json:"Protocol"`
	ID                      string        `json:"Id"`
	Path                    string        `json:"Path,omitempty"`
	Type                    string        `json:"Type"`
	Container               string        `json:"Container,omitempty"`
	Size                    int64         `json:"Size,omitempty"`
	Name                    string        `json:"Name"`
	IsRemote                bool          `json:"IsRemote"`
	RunTimeTicks            int64         `json:"RunTimeTicks,omitempty"`
	SupportsTranscoding     bool          `json:"SupportsTranscoding"`
	SupportsDirectStream    bool          `json:"SupportsDirectStream"`
	SupportsDirectPlay      bool          `json:"SupportsDirectPlay"`
	IsInfiniteStream        bool          `json:"IsInfiniteStream"`
	RequiresOpening         bool          `json:"RequiresOpening"`
	RequiresClosing         bool          `json:"RequiresClosing"`
	MediaStreams             []MediaStream `json:"MediaStreams,omitempty"`
	TranscodingURL          string        `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol  string        `json:"TranscodingSubProtocol,omitempty"`
	TranscodingContainer    string        `json:"TranscodingContainer,omitempty"`
	DefaultAudioStreamIndex int           `json:"DefaultAudioStreamIndex,omitempty"`
}

type MediaStream struct {
	Codec         string  `json:"Codec"`
	Language      string  `json:"Language,omitempty"`
	DisplayTitle  string  `json:"DisplayTitle,omitempty"`
	Type          string  `json:"Type"`
	Index         int     `json:"Index"`
	IsDefault     bool    `json:"IsDefault"`
	IsForced      bool    `json:"IsForced"`
	IsExternal    bool    `json:"IsExternal"`
	Height        int     `json:"Height,omitempty"`
	Width         int     `json:"Width,omitempty"`
	BitRate       int     `json:"BitRate,omitempty"`
	Channels      int     `json:"Channels,omitempty"`
	SampleRate    int     `json:"SampleRate,omitempty"`
	RealFrameRate float64 `json:"RealFrameRate,omitempty"`
	AspectRatio   string  `json:"AspectRatio,omitempty"`
	PixelFormat   string  `json:"PixelFormat,omitempty"`
	Level         float64 `json:"Level,omitempty"`
	Profile       string  `json:"Profile,omitempty"`
	VideoRange    string  `json:"VideoRange,omitempty"`
	VideoRangeType string `json:"VideoRangeType,omitempty"`
}

type BaseItemDtoQueryResult struct {
	Items            []BaseItemDto `json:"Items"`
	TotalRecordCount int           `json:"TotalRecordCount"`
	StartIndex       int           `json:"StartIndex"`
}

type PersonDto struct {
	Name     string `json:"Name"`
	ID       string `json:"Id"`
	Role     string `json:"Role,omitempty"`
	Type     string `json:"Type"`
	ImageTag string `json:"PrimaryImageTag,omitempty"`
}

type NameIDPair struct {
	Name string `json:"Name"`
	ID   string `json:"Id,omitempty"`
}

type BrandingConfiguration struct {
	LoginDisclaimer     string `json:"LoginDisclaimer,omitempty"`
	CustomCSS           string `json:"CustomCss,omitempty"`
	SplashscreenEnabled bool   `json:"SplashscreenEnabled"`
}

type DisplayPreferencesCustomPrefs struct {
	ChromecastVersion          string  `json:"chromecastVersion"`
	SkipForwardLength          string  `json:"skipForwardLength"`
	SkipBackLength             string  `json:"skipBackLength"`
	EnableNextVideoInfoOverlay string  `json:"enableNextVideoInfoOverlay"`
	TVHome                     *string `json:"tvhome"`
	DashboardTheme             *string `json:"dashboardTheme"`
}

type DisplayPreferences struct {
	ID                 string                        `json:"Id"`
	SortBy             string                        `json:"SortBy"`
	RememberIndexing   bool                          `json:"RememberIndexing"`
	PrimaryImageHeight int                           `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int                           `json:"PrimaryImageWidth"`
	CustomPrefs        DisplayPreferencesCustomPrefs `json:"CustomPrefs"`
	ScrollDirection    string                        `json:"ScrollDirection"`
	ShowBackdrop       bool                          `json:"ShowBackdrop"`
	RememberSorting    bool                          `json:"RememberSorting"`
	SortOrder          string                        `json:"SortOrder"`
	ShowSidebar        bool                          `json:"ShowSidebar"`
	Client             string                        `json:"Client"`
}

type EndpointInfo struct {
	IsLocal     bool `json:"IsLocal"`
	IsInNetwork bool `json:"IsInNetwork"`
}

type StorageDrive struct {
	Name         string `json:"Name"`
	Path         string `json:"Path"`
	Type         string `json:"Type"`
	FreeSpaceGB  int    `json:"FreeSpaceGB"`
	TotalSpaceGB int    `json:"TotalSpaceGB"`
}

type StorageInfo struct {
	Drives []StorageDrive `json:"Drives"`
}
