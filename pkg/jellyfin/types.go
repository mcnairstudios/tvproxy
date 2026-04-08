package jellyfin

import "time"

type PublicSystemInfo struct {
	LocalAddress             string `json:"LocalAddress"`
	ServerName               string `json:"ServerName"`
	Version                  string `json:"Version"`
	ProductName              string `json:"ProductName"`
	OperatingSystem          string `json:"OperatingSystem"`
	ID                       string `json:"Id"`
	StartupWizardCompleted   bool   `json:"StartupWizardCompleted"`
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
	TranscodingTempPath        string `json:"TranscodingTempPath"`
	ServerName                 string `json:"ServerName"`
}

type AuthenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

type AuthenticationResult struct {
	User        *UserDto     `json:"User"`
	SessionInfo *SessionInfo `json:"SessionInfo"`
	AccessToken string       `json:"AccessToken"`
	ServerID    string       `json:"ServerId"`
}

type UserDto struct {
	Name                    string          `json:"Name"`
	ServerID                string          `json:"ServerId"`
	ServerName              string          `json:"ServerName"`
	ID                      string          `json:"Id"`
	PrimaryImageTag         string          `json:"PrimaryImageTag,omitempty"`
	HasPassword             bool            `json:"HasPassword"`
	HasConfiguredPassword   bool            `json:"HasConfiguredPassword"`
	EnableAutoLogin         bool            `json:"EnableAutoLogin"`
	LastLoginDate           *time.Time      `json:"LastLoginDate,omitempty"`
	LastActivityDate        *time.Time      `json:"LastActivityDate,omitempty"`
	Configuration           UserConfig      `json:"Configuration"`
	Policy                  UserPolicy      `json:"Policy"`
}

type UserConfig struct {
	PlayDefaultAudioTrack    bool   `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference string `json:"SubtitleLanguagePreference"`
	DisplayMissingEpisodes   bool   `json:"DisplayMissingEpisodes"`
	SubtitleMode             string `json:"SubtitleMode"`
	EnableLocalPassword      bool   `json:"EnableLocalPassword"`
	HidePlayedInLatest       bool   `json:"HidePlayedInLatest"`
	RememberAudioSelections  bool   `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool  `json:"RememberSubtitleSelections"`
}

type UserPolicy struct {
	IsAdministrator                bool     `json:"IsAdministrator"`
	IsHidden                       bool     `json:"IsHidden"`
	IsDisabled                     bool     `json:"IsDisabled"`
	EnableUserPreferenceAccess     bool     `json:"EnableUserPreferenceAccess"`
	EnableRemoteControlOfOtherUsers bool    `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl      bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess             bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement         bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess             bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback            bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing         bool     `json:"EnablePlaybackRemuxing"`
	EnableContentDownloading       bool     `json:"EnableContentDownloading"`
	EnableAllChannels              bool     `json:"EnableAllChannels"`
	EnableAllFolders               bool     `json:"EnableAllFolders"`
	EnableAllDevices               bool     `json:"EnableAllDevices"`
	EnablePublicSharing            bool     `json:"EnablePublicSharing"`
	AuthenticationProviderId       string   `json:"AuthenticationProviderId"`
	PasswordResetProviderId        string   `json:"PasswordResetProviderId"`
}

type SessionInfo struct {
	PlayState               *PlayState `json:"PlayState,omitempty"`
	ID                      string     `json:"Id"`
	UserID                  string     `json:"UserId"`
	UserName                string     `json:"UserName"`
	Client                  string     `json:"Client"`
	LastActivityDate        time.Time  `json:"LastActivityDate"`
	DeviceName              string     `json:"DeviceName"`
	DeviceID                string     `json:"DeviceId"`
	ApplicationVersion      string     `json:"ApplicationVersion"`
	IsActive                bool       `json:"IsActive"`
	SupportsMediaControl    bool       `json:"SupportsMediaControl"`
	SupportsRemoteControl   bool       `json:"SupportsRemoteControl"`
	HasCustomDeviceName     bool       `json:"HasCustomDeviceName"`
	ServerID                string     `json:"ServerId"`
}

type PlayState struct {
	CanSeek          bool   `json:"CanSeek"`
	IsPaused         bool   `json:"IsPaused"`
	IsMuted          bool   `json:"IsMuted"`
	RepeatMode       string `json:"RepeatMode"`
	PlaybackOrder    string `json:"PlaybackOrder"`
}

type BaseItemDto struct {
	Name                    string          `json:"Name"`
	ServerID                string          `json:"ServerId"`
	ID                      string          `json:"Id"`
	Etag                    string          `json:"Etag,omitempty"`
	DateCreated             string          `json:"DateCreated,omitempty"`
	Container               string          `json:"Container,omitempty"`
	SortName                string          `json:"SortName,omitempty"`
	PremiereDate            string          `json:"PremiereDate,omitempty"`
	Path                    string          `json:"Path,omitempty"`
	OfficialRating          string          `json:"OfficialRating,omitempty"`
	Overview                string          `json:"Overview,omitempty"`
	Genres                  []string        `json:"Genres,omitempty"`
	CommunityRating         float64         `json:"CommunityRating,omitempty"`
	RunTimeTicks            int64           `json:"RunTimeTicks,omitempty"`
	ProductionYear          int             `json:"ProductionYear,omitempty"`
	IsFolder                bool            `json:"IsFolder"`
	Type                    string          `json:"Type"`
	CollectionType          string          `json:"CollectionType,omitempty"`
	ParentID                string          `json:"ParentId,omitempty"`
	ImageTags               map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags       []string        `json:"BackdropImageTags,omitempty"`
	LocationType            string          `json:"LocationType,omitempty"`
	MediaType               string          `json:"MediaType,omitempty"`
	Width                   int             `json:"Width,omitempty"`
	Height                  int             `json:"Height,omitempty"`
	ChildCount              int             `json:"ChildCount,omitempty"`
	SeriesName              string          `json:"SeriesName,omitempty"`
	SeriesID                string          `json:"SeriesId,omitempty"`
	SeasonID                string          `json:"SeasonId,omitempty"`
	IndexNumber             int             `json:"IndexNumber,omitempty"`
	ParentIndexNumber       int             `json:"ParentIndexNumber,omitempty"`
	UserData                *UserItemData   `json:"UserData,omitempty"`
	MediaSources            []MediaSource   `json:"MediaSources,omitempty"`
	Taglines                []string        `json:"Taglines,omitempty"`
	People                  []PersonDto     `json:"People,omitempty"`
	Studios                 []NameIDPair    `json:"Studios,omitempty"`
	ChannelNumber           string          `json:"ChannelNumber,omitempty"`
	ChannelPrimaryImageTag  string          `json:"ChannelPrimaryImageTag,omitempty"`
	CurrentProgram          *BaseItemDto    `json:"CurrentProgram,omitempty"`
}

type UserItemData struct {
	PlaybackPositionTicks int64  `json:"PlaybackPositionTicks"`
	PlayCount             int    `json:"PlayCount"`
	IsFavorite            bool   `json:"IsFavorite"`
	Played                bool   `json:"Played"`
	Key                   string `json:"Key"`
}

type MediaSource struct {
	Protocol             string        `json:"Protocol"`
	ID                   string        `json:"Id"`
	Path                 string        `json:"Path,omitempty"`
	Type                 string        `json:"Type"`
	Container            string        `json:"Container,omitempty"`
	Size                 int64         `json:"Size,omitempty"`
	Name                 string        `json:"Name"`
	IsRemote             bool          `json:"IsRemote"`
	RunTimeTicks         int64         `json:"RunTimeTicks,omitempty"`
	SupportsTranscoding  bool          `json:"SupportsTranscoding"`
	SupportsDirectStream bool          `json:"SupportsDirectStream"`
	SupportsDirectPlay   bool          `json:"SupportsDirectPlay"`
	IsInfiniteStream     bool          `json:"IsInfiniteStream"`
	RequiresOpening      bool          `json:"RequiresOpening"`
	RequiresClosing      bool          `json:"RequiresClosing"`
	MediaStreams          []MediaStream `json:"MediaStreams,omitempty"`
	TranscodingURL       string        `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol string      `json:"TranscodingSubProtocol,omitempty"`
	TranscodingContainer string        `json:"TranscodingContainer,omitempty"`
	DefaultAudioStreamIndex int        `json:"DefaultAudioStreamIndex,omitempty"`
}

type MediaStream struct {
	Codec             string `json:"Codec"`
	Language          string `json:"Language,omitempty"`
	DisplayTitle      string `json:"DisplayTitle,omitempty"`
	Type              string `json:"Type"`
	Index             int    `json:"Index"`
	IsDefault         bool   `json:"IsDefault"`
	IsForced          bool   `json:"IsForced"`
	IsExternal        bool   `json:"IsExternal"`
	Height            int    `json:"Height,omitempty"`
	Width             int    `json:"Width,omitempty"`
	BitRate           int    `json:"BitRate,omitempty"`
	Channels          int    `json:"Channels,omitempty"`
	SampleRate        int    `json:"SampleRate,omitempty"`
	RealFrameRate     float64 `json:"RealFrameRate,omitempty"`
	AspectRatio       string `json:"AspectRatio,omitempty"`
	PixelFormat       string `json:"PixelFormat,omitempty"`
	Level             float64 `json:"Level,omitempty"`
	Profile           string `json:"Profile,omitempty"`
	VideoRange        string `json:"VideoRange,omitempty"`
	VideoRangeType    string `json:"VideoRangeType,omitempty"`
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
	LoginDisclaimer string `json:"LoginDisclaimer"`
	CustomCSS       string `json:"CustomCss"`
	SplashscreenEnabled bool `json:"SplashscreenEnabled"`
}
