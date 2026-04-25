package jellyfinsdk

import "time"

// --- Enum types (string constants matching Kotlin SDK @SerialName values) ---

// SDKBaseItemKind maps to org.jellyfin.sdk.model.api.BaseItemKind
type SDKBaseItemKind string

const (
	BaseItemKindAggregateFolder      SDKBaseItemKind = "AggregateFolder"
	BaseItemKindAudio                SDKBaseItemKind = "Audio"
	BaseItemKindAudioBook            SDKBaseItemKind = "AudioBook"
	BaseItemKindBasePluginFolder     SDKBaseItemKind = "BasePluginFolder"
	BaseItemKindBook                 SDKBaseItemKind = "Book"
	BaseItemKindBoxSet               SDKBaseItemKind = "BoxSet"
	BaseItemKindChannel              SDKBaseItemKind = "Channel"
	BaseItemKindChannelFolderItem    SDKBaseItemKind = "ChannelFolderItem"
	BaseItemKindCollectionFolder     SDKBaseItemKind = "CollectionFolder"
	BaseItemKindEpisode              SDKBaseItemKind = "Episode"
	BaseItemKindFolder               SDKBaseItemKind = "Folder"
	BaseItemKindGenre                SDKBaseItemKind = "Genre"
	BaseItemKindManualPlaylistsFolder SDKBaseItemKind = "ManualPlaylistsFolder"
	BaseItemKindMovie                SDKBaseItemKind = "Movie"
	BaseItemKindLiveTvChannel        SDKBaseItemKind = "LiveTvChannel"
	BaseItemKindLiveTvProgram        SDKBaseItemKind = "LiveTvProgram"
	BaseItemKindMusicAlbum           SDKBaseItemKind = "MusicAlbum"
	BaseItemKindMusicArtist          SDKBaseItemKind = "MusicArtist"
	BaseItemKindMusicGenre           SDKBaseItemKind = "MusicGenre"
	BaseItemKindMusicVideo           SDKBaseItemKind = "MusicVideo"
	BaseItemKindPerson               SDKBaseItemKind = "Person"
	BaseItemKindPhoto                SDKBaseItemKind = "Photo"
	BaseItemKindPhotoAlbum           SDKBaseItemKind = "PhotoAlbum"
	BaseItemKindPlaylist             SDKBaseItemKind = "Playlist"
	BaseItemKindPlaylistsFolder      SDKBaseItemKind = "PlaylistsFolder"
	BaseItemKindProgram              SDKBaseItemKind = "Program"
	BaseItemKindRecording            SDKBaseItemKind = "Recording"
	BaseItemKindSeason               SDKBaseItemKind = "Season"
	BaseItemKindSeries               SDKBaseItemKind = "Series"
	BaseItemKindStudio               SDKBaseItemKind = "Studio"
	BaseItemKindTrailer              SDKBaseItemKind = "Trailer"
	BaseItemKindTvChannel            SDKBaseItemKind = "TvChannel"
	BaseItemKindTvProgram            SDKBaseItemKind = "TvProgram"
	BaseItemKindUserRootFolder       SDKBaseItemKind = "UserRootFolder"
	BaseItemKindUserView             SDKBaseItemKind = "UserView"
	BaseItemKindVideo                SDKBaseItemKind = "Video"
	BaseItemKindYear                 SDKBaseItemKind = "Year"
)

// SDKCollectionType maps to org.jellyfin.sdk.model.api.CollectionType
type SDKCollectionType string

const (
	CollectionTypeUnknown     SDKCollectionType = "unknown"
	CollectionTypeMovies      SDKCollectionType = "movies"
	CollectionTypeTvShows     SDKCollectionType = "tvshows"
	CollectionTypeMusic       SDKCollectionType = "music"
	CollectionTypeMusicVideos SDKCollectionType = "musicvideos"
	CollectionTypeTrailers    SDKCollectionType = "trailers"
	CollectionTypeHomeVideos  SDKCollectionType = "homevideos"
	CollectionTypeBoxSets     SDKCollectionType = "boxsets"
	CollectionTypeBooks       SDKCollectionType = "books"
	CollectionTypePhotos      SDKCollectionType = "photos"
	CollectionTypeLiveTv      SDKCollectionType = "livetv"
	CollectionTypePlaylists   SDKCollectionType = "playlists"
	CollectionTypeFolders     SDKCollectionType = "folders"
)

// SDKMediaType maps to org.jellyfin.sdk.model.api.MediaType
type SDKMediaType string

const (
	MediaTypeUnknown SDKMediaType = "Unknown"
	MediaTypeVideo   SDKMediaType = "Video"
	MediaTypeAudio   SDKMediaType = "Audio"
	MediaTypePhoto   SDKMediaType = "Photo"
	MediaTypeBook    SDKMediaType = "Book"
)

// SDKImageType maps to org.jellyfin.sdk.model.api.ImageType
type SDKImageType string

const (
	ImageTypePrimary    SDKImageType = "Primary"
	ImageTypeArt        SDKImageType = "Art"
	ImageTypeBackdrop   SDKImageType = "Backdrop"
	ImageTypeBanner     SDKImageType = "Banner"
	ImageTypeLogo       SDKImageType = "Logo"
	ImageTypeThumb      SDKImageType = "Thumb"
	ImageTypeDisc       SDKImageType = "Disc"
	ImageTypeBox        SDKImageType = "Box"
	ImageTypeScreenshot SDKImageType = "Screenshot"
	ImageTypeMenu       SDKImageType = "Menu"
	ImageTypeChapter    SDKImageType = "Chapter"
	ImageTypeBoxRear    SDKImageType = "BoxRear"
	ImageTypeProfile    SDKImageType = "Profile"
)

// SDKMediaStreamType maps to org.jellyfin.sdk.model.api.MediaStreamType
type SDKMediaStreamType string

const (
	MediaStreamTypeAudio         SDKMediaStreamType = "Audio"
	MediaStreamTypeVideo         SDKMediaStreamType = "Video"
	MediaStreamTypeSubtitle      SDKMediaStreamType = "Subtitle"
	MediaStreamTypeEmbeddedImage SDKMediaStreamType = "EmbeddedImage"
	MediaStreamTypeData          SDKMediaStreamType = "Data"
	MediaStreamTypeLyric         SDKMediaStreamType = "Lyric"
)

// SDKLocationType maps to org.jellyfin.sdk.model.api.LocationType
type SDKLocationType string

const (
	LocationTypeFileSystem SDKLocationType = "FileSystem"
	LocationTypeRemote     SDKLocationType = "Remote"
	LocationTypeVirtual    SDKLocationType = "Virtual"
	LocationTypeOffline    SDKLocationType = "Offline"
)

// SDKPersonKind maps to org.jellyfin.sdk.model.api.PersonKind
type SDKPersonKind string

const (
	PersonKindUnknown     SDKPersonKind = "Unknown"
	PersonKindActor       SDKPersonKind = "Actor"
	PersonKindDirector    SDKPersonKind = "Director"
	PersonKindComposer    SDKPersonKind = "Composer"
	PersonKindWriter      SDKPersonKind = "Writer"
	PersonKindGuestStar   SDKPersonKind = "GuestStar"
	PersonKindProducer    SDKPersonKind = "Producer"
	PersonKindConductor   SDKPersonKind = "Conductor"
	PersonKindLyricist    SDKPersonKind = "Lyricist"
	PersonKindArranger    SDKPersonKind = "Arranger"
	PersonKindEngineer    SDKPersonKind = "Engineer"
	PersonKindMixer       SDKPersonKind = "Mixer"
	PersonKindRemixer     SDKPersonKind = "Remixer"
	PersonKindCreator     SDKPersonKind = "Creator"
	PersonKindArtist      SDKPersonKind = "Artist"
	PersonKindAlbumArtist SDKPersonKind = "AlbumArtist"
	PersonKindAuthor      SDKPersonKind = "Author"
	PersonKindIllustrator SDKPersonKind = "Illustrator"
	PersonKindPenciller   SDKPersonKind = "Penciller"
	PersonKindInker       SDKPersonKind = "Inker"
	PersonKindColorist    SDKPersonKind = "Colorist"
	PersonKindLetterer    SDKPersonKind = "Letterer"
	PersonKindCoverArtist SDKPersonKind = "CoverArtist"
	PersonKindEditor      SDKPersonKind = "Editor"
	PersonKindTranslator  SDKPersonKind = "Translator"
)

// SDKMediaProtocol maps to org.jellyfin.sdk.model.api.MediaProtocol
type SDKMediaProtocol string

const (
	MediaProtocolFile SDKMediaProtocol = "File"
	MediaProtocolHTTP SDKMediaProtocol = "Http"
	MediaProtocolRTMP SDKMediaProtocol = "Rtmp"
	MediaProtocolRTSP SDKMediaProtocol = "Rtsp"
	MediaProtocolUDP  SDKMediaProtocol = "Udp"
	MediaProtocolRTP  SDKMediaProtocol = "Rtp"
	MediaProtocolFTP  SDKMediaProtocol = "Ftp"
)

// SDKMediaSourceType maps to org.jellyfin.sdk.model.api.MediaSourceType
type SDKMediaSourceType string

const (
	MediaSourceTypeDefault     SDKMediaSourceType = "Default"
	MediaSourceTypeGrouping    SDKMediaSourceType = "Grouping"
	MediaSourceTypePlaceholder SDKMediaSourceType = "Placeholder"
)

// SDKChannelType maps to org.jellyfin.sdk.model.api.ChannelType
type SDKChannelType string

const (
	ChannelTypeTV    SDKChannelType = "TV"
	ChannelTypeRadio SDKChannelType = "Radio"
)

// SDKScrollDirection maps to org.jellyfin.sdk.model.api.ScrollDirection
type SDKScrollDirection string

const (
	ScrollDirectionHorizontal SDKScrollDirection = "Horizontal"
	ScrollDirectionVertical   SDKScrollDirection = "Vertical"
)

// SDKSortOrder maps to org.jellyfin.sdk.model.api.SortOrder
type SDKSortOrder string

const (
	SortOrderAscending  SDKSortOrder = "Ascending"
	SortOrderDescending SDKSortOrder = "Descending"
)

// SDKVideoRange maps to org.jellyfin.sdk.model.api.VideoRange
type SDKVideoRange string

const (
	VideoRangeUnknown SDKVideoRange = "Unknown"
	VideoRangeSDR     SDKVideoRange = "SDR"
	VideoRangeHDR     SDKVideoRange = "HDR"
)

// SDKVideoRangeType maps to org.jellyfin.sdk.model.api.VideoRangeType
type SDKVideoRangeType string

const (
	VideoRangeTypeUnknown  SDKVideoRangeType = "Unknown"
	VideoRangeTypeSDR      SDKVideoRangeType = "SDR"
	VideoRangeTypeHDR10    SDKVideoRangeType = "HDR10"
	VideoRangeTypeHLG      SDKVideoRangeType = "HLG"
	VideoRangeTypeDOVI     SDKVideoRangeType = "DOVI"
	VideoRangeTypeDOVIWithHDR10 SDKVideoRangeType = "DOVIWithHDR10"
	VideoRangeTypeDOVIWithHLG   SDKVideoRangeType = "DOVIWithHLG"
	VideoRangeTypeDOVIWithSDR   SDKVideoRangeType = "DOVIWithSDR"
	VideoRangeTypeHDR10Plus     SDKVideoRangeType = "HDR10Plus"
)

// --- Supporting structs ---

// SDKExternalUrl maps to org.jellyfin.sdk.model.api.ExternalUrl
type SDKExternalUrl struct {
	Name *string `json:"Name,omitempty"`
	URL  *string `json:"Url,omitempty"`
}

// SDKMediaUrl maps to org.jellyfin.sdk.model.api.MediaUrl
type SDKMediaUrl struct {
	URL  *string `json:"Url,omitempty"`
	Name *string `json:"Name,omitempty"`
}

// SDKNameGuidPair maps to org.jellyfin.sdk.model.api.NameGuidPair
type SDKNameGuidPair struct {
	Name *string `json:"Name,omitempty"`
	ID   string  `json:"Id"`
}

// SDKChapterInfo maps to org.jellyfin.sdk.model.api.ChapterInfo
type SDKChapterInfo struct {
	StartPositionTicks int64      `json:"StartPositionTicks"`
	Name               *string    `json:"Name,omitempty"`
	ImagePath          *string    `json:"ImagePath,omitempty"`
	ImageDateModified  time.Time  `json:"ImageDateModified"`
	ImageTag           *string    `json:"ImageTag,omitempty"`
}

// --- Primary model structs ---

// SDKBaseItemPerson maps to org.jellyfin.sdk.model.api.BaseItemPerson
type SDKBaseItemPerson struct {
	Name            *string                              `json:"Name,omitempty"`
	ID              string                               `json:"Id"`
	Role            *string                              `json:"Role,omitempty"`
	Type            SDKPersonKind                        `json:"Type"`
	PrimaryImageTag *string                              `json:"PrimaryImageTag,omitempty"`
	ImageBlurHashes map[SDKImageType]map[string]string   `json:"ImageBlurHashes,omitempty"`
}

// SDKUserItemDataDto maps to org.jellyfin.sdk.model.api.UserItemDataDto
type SDKUserItemDataDto struct {
	Rating                *float64   `json:"Rating,omitempty"`
	PlayedPercentage      *float64   `json:"PlayedPercentage,omitempty"`
	UnplayedItemCount     *int       `json:"UnplayedItemCount,omitempty"`
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	PlayCount             int        `json:"PlayCount"`
	IsFavorite            bool       `json:"IsFavorite"`
	Likes                 *bool      `json:"Likes,omitempty"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
	Played                bool       `json:"Played"`
	Key                   string     `json:"Key"`
	ItemID                string     `json:"ItemId"`
}

// SDKMediaStream maps to org.jellyfin.sdk.model.api.MediaStream
type SDKMediaStream struct {
	Codec                    *string            `json:"Codec,omitempty"`
	CodecTag                 *string            `json:"CodecTag,omitempty"`
	Language                 *string            `json:"Language,omitempty"`
	ColorRange               *string            `json:"ColorRange,omitempty"`
	ColorSpace               *string            `json:"ColorSpace,omitempty"`
	ColorTransfer            *string            `json:"ColorTransfer,omitempty"`
	ColorPrimaries           *string            `json:"ColorPrimaries,omitempty"`
	DvVersionMajor           *int               `json:"DvVersionMajor,omitempty"`
	DvVersionMinor           *int               `json:"DvVersionMinor,omitempty"`
	DvProfile                *int               `json:"DvProfile,omitempty"`
	DvLevel                  *int               `json:"DvLevel,omitempty"`
	RpuPresentFlag           *int               `json:"RpuPresentFlag,omitempty"`
	ElPresentFlag            *int               `json:"ElPresentFlag,omitempty"`
	BlPresentFlag            *int               `json:"BlPresentFlag,omitempty"`
	DvBlSignalCompatibilityID *int              `json:"DvBlSignalCompatibilityId,omitempty"`
	Rotation                 *int               `json:"Rotation,omitempty"`
	Comment                  *string            `json:"Comment,omitempty"`
	TimeBase                 *string            `json:"TimeBase,omitempty"`
	CodecTimeBase            *string            `json:"CodecTimeBase,omitempty"`
	Title                    *string            `json:"Title,omitempty"`
	Hdr10PlusPresentFlag     *bool              `json:"Hdr10PlusPresentFlag,omitempty"`
	VideoRange               SDKVideoRange      `json:"VideoRange"`
	VideoRangeType           SDKVideoRangeType  `json:"VideoRangeType"`
	VideoDoViTitle           *string            `json:"VideoDoViTitle,omitempty"`
	AudioSpatialFormat       string             `json:"AudioSpatialFormat"`
	LocalizedUndefined       *string            `json:"LocalizedUndefined,omitempty"`
	LocalizedDefault         *string            `json:"LocalizedDefault,omitempty"`
	LocalizedForced          *string            `json:"LocalizedForced,omitempty"`
	LocalizedExternal        *string            `json:"LocalizedExternal,omitempty"`
	LocalizedHearingImpaired *string            `json:"LocalizedHearingImpaired,omitempty"`
	DisplayTitle             *string            `json:"DisplayTitle,omitempty"`
	NalLengthSize            *string            `json:"NalLengthSize,omitempty"`
	IsInterlaced             bool               `json:"IsInterlaced"`
	IsAVC                    *bool              `json:"IsAVC,omitempty"`
	ChannelLayout            *string            `json:"ChannelLayout,omitempty"`
	BitRate                  *int               `json:"BitRate,omitempty"`
	BitDepth                 *int               `json:"BitDepth,omitempty"`
	RefFrames                *int               `json:"RefFrames,omitempty"`
	PacketLength             *int               `json:"PacketLength,omitempty"`
	Channels                 *int               `json:"Channels,omitempty"`
	SampleRate               *int               `json:"SampleRate,omitempty"`
	IsDefault                bool               `json:"IsDefault"`
	IsForced                 bool               `json:"IsForced"`
	IsHearingImpaired        bool               `json:"IsHearingImpaired"`
	Height                   *int               `json:"Height,omitempty"`
	Width                    *int               `json:"Width,omitempty"`
	AverageFrameRate         *float32           `json:"AverageFrameRate,omitempty"`
	RealFrameRate            *float32           `json:"RealFrameRate,omitempty"`
	ReferenceFrameRate       *float32           `json:"ReferenceFrameRate,omitempty"`
	Profile                  *string            `json:"Profile,omitempty"`
	Type                     SDKMediaStreamType `json:"Type"`
	AspectRatio              *string            `json:"AspectRatio,omitempty"`
	Index                    int                `json:"Index"`
	Score                    *int               `json:"Score,omitempty"`
	IsExternal               bool               `json:"IsExternal"`
	DeliveryMethod           *string            `json:"DeliveryMethod,omitempty"`
	DeliveryURL              *string            `json:"DeliveryUrl,omitempty"`
	IsExternalURL            *bool              `json:"IsExternalUrl,omitempty"`
	IsTextSubtitleStream     bool               `json:"IsTextSubtitleStream"`
	SupportsExternalStream   bool               `json:"SupportsExternalStream"`
	Path                     *string            `json:"Path,omitempty"`
	PixelFormat              *string            `json:"PixelFormat,omitempty"`
	Level                    *float64           `json:"Level,omitempty"`
	IsAnamorphic             *bool              `json:"IsAnamorphic,omitempty"`
}

// SDKMediaSourceInfo maps to org.jellyfin.sdk.model.api.MediaSourceInfo
type SDKMediaSourceInfo struct {
	Protocol                              SDKMediaProtocol   `json:"Protocol"`
	ID                                    *string            `json:"Id,omitempty"`
	Path                                  *string            `json:"Path,omitempty"`
	EncoderPath                           *string            `json:"EncoderPath,omitempty"`
	EncoderProtocol                       *string            `json:"EncoderProtocol,omitempty"`
	Type                                  SDKMediaSourceType `json:"Type"`
	Container                             *string            `json:"Container,omitempty"`
	Size                                  *int64             `json:"Size,omitempty"`
	Name                                  *string            `json:"Name,omitempty"`
	IsRemote                              bool               `json:"IsRemote"`
	ETag                                  *string            `json:"ETag,omitempty"`
	RunTimeTicks                          *int64             `json:"RunTimeTicks,omitempty"`
	ReadAtNativeFramerate                 bool               `json:"ReadAtNativeFramerate"`
	IgnoreDts                             bool               `json:"IgnoreDts"`
	IgnoreIndex                           bool               `json:"IgnoreIndex"`
	GenPtsInput                           bool               `json:"GenPtsInput"`
	SupportsTranscoding                   bool               `json:"SupportsTranscoding"`
	SupportsDirectStream                  bool               `json:"SupportsDirectStream"`
	SupportsDirectPlay                    bool               `json:"SupportsDirectPlay"`
	IsInfiniteStream                      bool               `json:"IsInfiniteStream"`
	UseMostCompatibleTranscodingProfile   bool               `json:"UseMostCompatibleTranscodingProfile"`
	RequiresOpening                       bool               `json:"RequiresOpening"`
	OpenToken                             *string            `json:"OpenToken,omitempty"`
	RequiresClosing                       bool               `json:"RequiresClosing"`
	LiveStreamID                          *string            `json:"LiveStreamId,omitempty"`
	BufferMs                              *int               `json:"BufferMs,omitempty"`
	RequiresLooping                       bool               `json:"RequiresLooping"`
	SupportsProbing                       bool               `json:"SupportsProbing"`
	VideoType                             *string            `json:"VideoType,omitempty"`
	IsoType                               *string            `json:"IsoType,omitempty"`
	Video3dFormat                         *string            `json:"Video3DFormat,omitempty"`
	MediaStreams                          []SDKMediaStream    `json:"MediaStreams,omitempty"`
	MediaAttachments                     []any               `json:"MediaAttachments,omitempty"`
	Formats                              []string            `json:"Formats,omitempty"`
	Bitrate                              *int                `json:"Bitrate,omitempty"`
	FallbackMaxStreamingBitrate          *int                `json:"FallbackMaxStreamingBitrate,omitempty"`
	Timestamp                            *string             `json:"Timestamp,omitempty"`
	RequiredHTTPHeaders                  map[string]*string  `json:"RequiredHttpHeaders,omitempty"`
	TranscodingURL                       *string             `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol               string              `json:"TranscodingSubProtocol"`
	TranscodingContainer                 *string             `json:"TranscodingContainer,omitempty"`
	AnalyzeDurationMs                    *int                `json:"AnalyzeDurationMs,omitempty"`
	DefaultAudioStreamIndex              *int                `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex           *int                `json:"DefaultSubtitleStreamIndex,omitempty"`
	HasSegments                          bool                `json:"HasSegments"`
}

// SDKBaseItemDto maps to org.jellyfin.sdk.model.api.BaseItemDto
// This is the primary item DTO used for movies, episodes, channels, views, etc.
type SDKBaseItemDto struct {
	Name                        *string                                      `json:"Name,omitempty"`
	OriginalTitle               *string                                      `json:"OriginalTitle,omitempty"`
	ServerID                    *string                                      `json:"ServerId,omitempty"`
	ID                          string                                       `json:"Id"`
	Etag                        *string                                      `json:"Etag,omitempty"`
	SourceType                  *string                                      `json:"SourceType,omitempty"`
	PlaylistItemID              *string                                      `json:"PlaylistItemId,omitempty"`
	DateCreated                 *time.Time                                   `json:"DateCreated,omitempty"`
	DateLastMediaAdded          *time.Time                                   `json:"DateLastMediaAdded,omitempty"`
	ExtraType                   *string                                      `json:"ExtraType,omitempty"`
	AirsBeforeSeasonNumber      *int                                         `json:"AirsBeforeSeasonNumber,omitempty"`
	AirsAfterSeasonNumber       *int                                         `json:"AirsAfterSeasonNumber,omitempty"`
	AirsBeforeEpisodeNumber     *int                                         `json:"AirsBeforeEpisodeNumber,omitempty"`
	CanDelete                   *bool                                        `json:"CanDelete,omitempty"`
	CanDownload                 *bool                                        `json:"CanDownload,omitempty"`
	HasLyrics                   *bool                                        `json:"HasLyrics,omitempty"`
	HasSubtitles                *bool                                        `json:"HasSubtitles,omitempty"`
	PreferredMetadataLanguage   *string                                      `json:"PreferredMetadataLanguage,omitempty"`
	PreferredMetadataCountryCode *string                                     `json:"PreferredMetadataCountryCode,omitempty"`
	Container                   *string                                      `json:"Container,omitempty"`
	SortName                    *string                                      `json:"SortName,omitempty"`
	ForcedSortName              *string                                      `json:"ForcedSortName,omitempty"`
	Video3dFormat               *string                                      `json:"Video3DFormat,omitempty"`
	PremiereDate                *time.Time                                   `json:"PremiereDate,omitempty"`
	ExternalUrls                []SDKExternalUrl                             `json:"ExternalUrls,omitempty"`
	MediaSources                []SDKMediaSourceInfo                         `json:"MediaSources,omitempty"`
	CriticRating                *float32                                     `json:"CriticRating,omitempty"`
	ProductionLocations         []string                                     `json:"ProductionLocations,omitempty"`
	Path                        *string                                      `json:"Path,omitempty"`
	EnableMediaSourceDisplay    *bool                                        `json:"EnableMediaSourceDisplay,omitempty"`
	OfficialRating              *string                                      `json:"OfficialRating,omitempty"`
	CustomRating                *string                                      `json:"CustomRating,omitempty"`
	ChannelID                   *string                                      `json:"ChannelId,omitempty"`
	ChannelName                 *string                                      `json:"ChannelName,omitempty"`
	Overview                    *string                                      `json:"Overview,omitempty"`
	Taglines                    []string                                     `json:"Taglines,omitempty"`
	Genres                      []string                                     `json:"Genres,omitempty"`
	CommunityRating             *float32                                     `json:"CommunityRating,omitempty"`
	CumulativeRunTimeTicks      *int64                                       `json:"CumulativeRunTimeTicks,omitempty"`
	RunTimeTicks                *int64                                       `json:"RunTimeTicks,omitempty"`
	PlayAccess                  *string                                      `json:"PlayAccess,omitempty"`
	AspectRatio                 *string                                      `json:"AspectRatio,omitempty"`
	ProductionYear              *int                                         `json:"ProductionYear,omitempty"`
	IsPlaceHolder               *bool                                        `json:"IsPlaceHolder,omitempty"`
	Number                      *string                                      `json:"Number,omitempty"`
	ChannelNumber               *string                                      `json:"ChannelNumber,omitempty"`
	IndexNumber                 *int                                         `json:"IndexNumber,omitempty"`
	IndexNumberEnd              *int                                         `json:"IndexNumberEnd,omitempty"`
	ParentIndexNumber           *int                                         `json:"ParentIndexNumber,omitempty"`
	RemoteTrailers              []SDKMediaUrl                                `json:"RemoteTrailers,omitempty"`
	ProviderIDs                 map[string]*string                           `json:"ProviderIds,omitempty"`
	IsHD                        *bool                                        `json:"IsHD,omitempty"`
	IsFolder                    *bool                                        `json:"IsFolder,omitempty"`
	ParentID                    *string                                      `json:"ParentId,omitempty"`
	Type                        SDKBaseItemKind                              `json:"Type"`
	People                      []SDKBaseItemPerson                          `json:"People,omitempty"`
	Studios                     []SDKNameGuidPair                            `json:"Studios,omitempty"`
	GenreItems                  []SDKNameGuidPair                            `json:"GenreItems,omitempty"`
	ParentLogoItemID            *string                                      `json:"ParentLogoItemId,omitempty"`
	ParentBackdropItemID        *string                                      `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags     []string                                     `json:"ParentBackdropImageTags,omitempty"`
	LocalTrailerCount           *int                                         `json:"LocalTrailerCount,omitempty"`
	UserData                    *SDKUserItemDataDto                          `json:"UserData,omitempty"`
	RecursiveItemCount          *int                                         `json:"RecursiveItemCount,omitempty"`
	ChildCount                  *int                                         `json:"ChildCount,omitempty"`
	SeriesName                  *string                                      `json:"SeriesName,omitempty"`
	SeriesID                    *string                                      `json:"SeriesId,omitempty"`
	SeasonID                    *string                                      `json:"SeasonId,omitempty"`
	SpecialFeatureCount         *int                                         `json:"SpecialFeatureCount,omitempty"`
	DisplayPreferencesID        *string                                      `json:"DisplayPreferencesId,omitempty"`
	Status                      *string                                      `json:"Status,omitempty"`
	AirTime                     *string                                      `json:"AirTime,omitempty"`
	AirDays                     []string                                     `json:"AirDays,omitempty"`
	Tags                        []string                                     `json:"Tags,omitempty"`
	PrimaryImageAspectRatio     *float64                                     `json:"PrimaryImageAspectRatio,omitempty"`
	Artists                     []string                                     `json:"Artists,omitempty"`
	ArtistItems                 []SDKNameGuidPair                            `json:"ArtistItems,omitempty"`
	Album                       *string                                      `json:"Album,omitempty"`
	CollectionType              *SDKCollectionType                           `json:"CollectionType,omitempty"`
	DisplayOrder                *string                                      `json:"DisplayOrder,omitempty"`
	AlbumID                     *string                                      `json:"AlbumId,omitempty"`
	AlbumPrimaryImageTag        *string                                      `json:"AlbumPrimaryImageTag,omitempty"`
	SeriesPrimaryImageTag       *string                                      `json:"SeriesPrimaryImageTag,omitempty"`
	AlbumArtist                 *string                                      `json:"AlbumArtist,omitempty"`
	AlbumArtists                []SDKNameGuidPair                            `json:"AlbumArtists,omitempty"`
	SeasonName                  *string                                      `json:"SeasonName,omitempty"`
	MediaStreams                 []SDKMediaStream                             `json:"MediaStreams,omitempty"`
	VideoType                   *string                                      `json:"VideoType,omitempty"`
	PartCount                   *int                                         `json:"PartCount,omitempty"`
	MediaSourceCount            *int                                         `json:"MediaSourceCount,omitempty"`
	ImageTags                   map[SDKImageType]string                      `json:"ImageTags,omitempty"`
	BackdropImageTags           []string                                     `json:"BackdropImageTags,omitempty"`
	ScreenshotImageTags         []string                                     `json:"ScreenshotImageTags,omitempty"`
	ParentLogoImageTag          *string                                      `json:"ParentLogoImageTag,omitempty"`
	ParentArtItemID             *string                                      `json:"ParentArtItemId,omitempty"`
	ParentArtImageTag           *string                                      `json:"ParentArtImageTag,omitempty"`
	SeriesThumbImageTag         *string                                      `json:"SeriesThumbImageTag,omitempty"`
	ImageBlurHashes             map[SDKImageType]map[string]string           `json:"ImageBlurHashes,omitempty"`
	SeriesStudio                *string                                      `json:"SeriesStudio,omitempty"`
	ParentThumbItemID           *string                                      `json:"ParentThumbItemId,omitempty"`
	ParentThumbImageTag         *string                                      `json:"ParentThumbImageTag,omitempty"`
	ParentPrimaryImageItemID    *string                                      `json:"ParentPrimaryImageItemId,omitempty"`
	ParentPrimaryImageTag       *string                                      `json:"ParentPrimaryImageTag,omitempty"`
	Chapters                    []SDKChapterInfo                             `json:"Chapters,omitempty"`
	Trickplay                   map[string]map[string]any                    `json:"Trickplay,omitempty"`
	LocationType                *SDKLocationType                             `json:"LocationType,omitempty"`
	IsoType                     *string                                      `json:"IsoType,omitempty"`
	MediaType                   SDKMediaType                                 `json:"MediaType"`
	EndDate                     *time.Time                                   `json:"EndDate,omitempty"`
	LockedFields                []string                                     `json:"LockedFields,omitempty"`
	TrailerCount                *int                                         `json:"TrailerCount,omitempty"`
	MovieCount                  *int                                         `json:"MovieCount,omitempty"`
	SeriesCount                 *int                                         `json:"SeriesCount,omitempty"`
	ProgramCount                *int                                         `json:"ProgramCount,omitempty"`
	EpisodeCount                *int                                         `json:"EpisodeCount,omitempty"`
	SongCount                   *int                                         `json:"SongCount,omitempty"`
	AlbumCount                  *int                                         `json:"AlbumCount,omitempty"`
	ArtistCount                 *int                                         `json:"ArtistCount,omitempty"`
	MusicVideoCount             *int                                         `json:"MusicVideoCount,omitempty"`
	LockData                    *bool                                        `json:"LockData,omitempty"`
	Width                       *int                                         `json:"Width,omitempty"`
	Height                      *int                                         `json:"Height,omitempty"`
	CameraMake                  *string                                      `json:"CameraMake,omitempty"`
	CameraModel                 *string                                      `json:"CameraModel,omitempty"`
	Software                    *string                                      `json:"Software,omitempty"`
	ExposureTime                *float64                                     `json:"ExposureTime,omitempty"`
	FocalLength                 *float64                                     `json:"FocalLength,omitempty"`
	ImageOrientation            *string                                      `json:"ImageOrientation,omitempty"`
	Aperture                    *float64                                     `json:"Aperture,omitempty"`
	ShutterSpeed                *float64                                     `json:"ShutterSpeed,omitempty"`
	Latitude                    *float64                                     `json:"Latitude,omitempty"`
	Longitude                   *float64                                     `json:"Longitude,omitempty"`
	Altitude                    *float64                                     `json:"Altitude,omitempty"`
	IsoSpeedRating              *int                                         `json:"IsoSpeedRating,omitempty"`
	SeriesTimerID               *string                                      `json:"SeriesTimerId,omitempty"`
	ProgramID                   *string                                      `json:"ProgramId,omitempty"`
	ChannelPrimaryImageTag      *string                                      `json:"ChannelPrimaryImageTag,omitempty"`
	StartDate                   *time.Time                                   `json:"StartDate,omitempty"`
	CompletionPercentage        *float64                                     `json:"CompletionPercentage,omitempty"`
	IsRepeat                    *bool                                        `json:"IsRepeat,omitempty"`
	EpisodeTitle                *string                                      `json:"EpisodeTitle,omitempty"`
	ChannelType                 *SDKChannelType                              `json:"ChannelType,omitempty"`
	Audio                       *string                                      `json:"Audio,omitempty"`
	IsMovie                     *bool                                        `json:"IsMovie,omitempty"`
	IsSports                    *bool                                        `json:"IsSports,omitempty"`
	IsSeries                    *bool                                        `json:"IsSeries,omitempty"`
	IsLive                      *bool                                        `json:"IsLive,omitempty"`
	IsNews                      *bool                                        `json:"IsNews,omitempty"`
	IsKids                      *bool                                        `json:"IsKids,omitempty"`
	IsPremiere                  *bool                                        `json:"IsPremiere,omitempty"`
	TimerID                     *string                                      `json:"TimerId,omitempty"`
	NormalizationGain           *float32                                     `json:"NormalizationGain,omitempty"`
	CurrentProgram              *SDKBaseItemDto                              `json:"CurrentProgram,omitempty"`
}

// SDKBaseItemDtoQueryResult maps to org.jellyfin.sdk.model.api.BaseItemDtoQueryResult
type SDKBaseItemDtoQueryResult struct {
	Items            []SDKBaseItemDto `json:"Items"`
	TotalRecordCount int              `json:"TotalRecordCount"`
	StartIndex       int              `json:"StartIndex"`
}

// SDKUserConfiguration maps to org.jellyfin.sdk.model.api.UserConfiguration
type SDKUserConfiguration struct {
	AudioLanguagePreference    *string  `json:"AudioLanguagePreference,omitempty"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference *string  `json:"SubtitleLanguagePreference,omitempty"`
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
	CastReceiverID             *string  `json:"CastReceiverId,omitempty"`
}

// SDKUserPolicy maps to org.jellyfin.sdk.model.api.UserPolicy
type SDKUserPolicy struct {
	IsAdministrator                  bool     `json:"IsAdministrator"`
	IsHidden                         bool     `json:"IsHidden"`
	EnableCollectionManagement       bool     `json:"EnableCollectionManagement"`
	EnableSubtitleManagement         bool     `json:"EnableSubtitleManagement"`
	EnableLyricManagement            bool     `json:"EnableLyricManagement"`
	IsDisabled                       bool     `json:"IsDisabled"`
	MaxParentalRating                *int     `json:"MaxParentalRating,omitempty"`
	MaxParentalSubRating             *int     `json:"MaxParentalSubRating,omitempty"`
	BlockedTags                      []string `json:"BlockedTags,omitempty"`
	AllowedTags                      []string `json:"AllowedTags,omitempty"`
	EnableUserPreferenceAccess       bool     `json:"EnableUserPreferenceAccess"`
	AccessSchedules                  []any    `json:"AccessSchedules,omitempty"`
	BlockUnratedItems                []string `json:"BlockUnratedItems,omitempty"`
	EnableRemoteControlOfOtherUsers  bool     `json:"EnableRemoteControlOfOtherUsers"`
	EnableSharedDeviceControl        bool     `json:"EnableSharedDeviceControl"`
	EnableRemoteAccess               bool     `json:"EnableRemoteAccess"`
	EnableLiveTvManagement           bool     `json:"EnableLiveTvManagement"`
	EnableLiveTvAccess               bool     `json:"EnableLiveTvAccess"`
	EnableMediaPlayback              bool     `json:"EnableMediaPlayback"`
	EnableAudioPlaybackTranscoding   bool     `json:"EnableAudioPlaybackTranscoding"`
	EnableVideoPlaybackTranscoding   bool     `json:"EnableVideoPlaybackTranscoding"`
	EnablePlaybackRemuxing           bool     `json:"EnablePlaybackRemuxing"`
	ForceRemoteSourceTranscoding     bool     `json:"ForceRemoteSourceTranscoding"`
	EnableContentDeletion            bool     `json:"EnableContentDeletion"`
	EnableContentDeletionFromFolders []string `json:"EnableContentDeletionFromFolders,omitempty"`
	EnableContentDownloading         bool     `json:"EnableContentDownloading"`
	EnableSyncTranscoding            bool     `json:"EnableSyncTranscoding"`
	EnableMediaConversion            bool     `json:"EnableMediaConversion"`
	EnabledDevices                   []string `json:"EnabledDevices,omitempty"`
	EnableAllDevices                 bool     `json:"EnableAllDevices"`
	EnabledChannels                  []string `json:"EnabledChannels,omitempty"`
	EnableAllChannels                bool     `json:"EnableAllChannels"`
	EnabledFolders                   []string `json:"EnabledFolders,omitempty"`
	EnableAllFolders                 bool     `json:"EnableAllFolders"`
	InvalidLoginAttemptCount         int      `json:"InvalidLoginAttemptCount"`
	LoginAttemptsBeforeLockout       int      `json:"LoginAttemptsBeforeLockout"`
	MaxActiveSessions                int      `json:"MaxActiveSessions"`
	EnablePublicSharing              bool     `json:"EnablePublicSharing"`
	BlockedMediaFolders              []string `json:"BlockedMediaFolders,omitempty"`
	BlockedChannels                  []string `json:"BlockedChannels,omitempty"`
	RemoteClientBitrateLimit         int      `json:"RemoteClientBitrateLimit"`
	AuthenticationProviderId         string   `json:"AuthenticationProviderId"`
	PasswordResetProviderId          string   `json:"PasswordResetProviderId"`
	SyncPlayAccess                   string   `json:"SyncPlayAccess"`
}

// SDKUserDto maps to org.jellyfin.sdk.model.api.UserDto
type SDKUserDto struct {
	Name                      *string               `json:"Name,omitempty"`
	ServerID                  *string               `json:"ServerId,omitempty"`
	ServerName                *string               `json:"ServerName,omitempty"`
	ID                        string                `json:"Id"`
	PrimaryImageTag           *string               `json:"PrimaryImageTag,omitempty"`
	HasPassword               bool                  `json:"HasPassword"`
	HasConfiguredPassword     bool                  `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool                  `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           *bool                 `json:"EnableAutoLogin,omitempty"`
	LastLoginDate             *time.Time            `json:"LastLoginDate,omitempty"`
	LastActivityDate          *time.Time            `json:"LastActivityDate,omitempty"`
	Configuration             *SDKUserConfiguration `json:"Configuration,omitempty"`
	Policy                    *SDKUserPolicy        `json:"Policy,omitempty"`
	PrimaryImageAspectRatio   *float64              `json:"PrimaryImageAspectRatio,omitempty"`
}

// SDKAuthenticationResult maps to org.jellyfin.sdk.model.api.AuthenticationResult
type SDKAuthenticationResult struct {
	User        *SDKUserDto        `json:"User,omitempty"`
	SessionInfo *SDKSessionInfoDto `json:"SessionInfo,omitempty"`
	AccessToken *string            `json:"AccessToken,omitempty"`
	ServerID    *string            `json:"ServerId,omitempty"`
}

// SDKPlayerStateInfo maps to org.jellyfin.sdk.model.api.PlayerStateInfo
type SDKPlayerStateInfo struct {
	PositionTicks      *int64  `json:"PositionTicks,omitempty"`
	CanSeek            bool    `json:"CanSeek"`
	IsPaused           bool    `json:"IsPaused"`
	IsMuted            bool    `json:"IsMuted"`
	VolumeLevel        *int    `json:"VolumeLevel,omitempty"`
	AudioStreamIndex   *int    `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex,omitempty"`
	MediaSourceID      *string `json:"MediaSourceId,omitempty"`
	PlayMethod         *string `json:"PlayMethod,omitempty"`
	RepeatMode         string  `json:"RepeatMode"`
	PlaybackOrder      string  `json:"PlaybackOrder"`
	LiveStreamID       *string `json:"LiveStreamId,omitempty"`
}

// SDKClientCapabilitiesDto maps to org.jellyfin.sdk.model.api.ClientCapabilitiesDto
type SDKClientCapabilitiesDto struct {
	PlayableMediaTypes          []string `json:"PlayableMediaTypes"`
	SupportedCommands           []string `json:"SupportedCommands"`
	SupportsMediaControl        bool     `json:"SupportsMediaControl"`
	SupportsPersistentIdentifier bool    `json:"SupportsPersistentIdentifier"`
	DeviceProfile               any      `json:"DeviceProfile,omitempty"`
	AppStoreURL                 *string  `json:"AppStoreUrl,omitempty"`
	IconURL                     *string  `json:"IconUrl,omitempty"`
}

// SDKSessionUserInfo maps to org.jellyfin.sdk.model.api.SessionUserInfo
type SDKSessionUserInfo struct {
	UserID   string `json:"UserId"`
	UserName *string `json:"UserName,omitempty"`
}

// SDKSessionInfoDto maps to org.jellyfin.sdk.model.api.SessionInfoDto
type SDKSessionInfoDto struct {
	PlayState                *SDKPlayerStateInfo       `json:"PlayState,omitempty"`
	AdditionalUsers          []SDKSessionUserInfo      `json:"AdditionalUsers,omitempty"`
	Capabilities             *SDKClientCapabilitiesDto `json:"Capabilities,omitempty"`
	RemoteEndPoint           *string                   `json:"RemoteEndPoint,omitempty"`
	PlayableMediaTypes       []string                  `json:"PlayableMediaTypes"`
	ID                       *string                   `json:"Id,omitempty"`
	UserID                   string                    `json:"UserId"`
	UserName                 *string                   `json:"UserName,omitempty"`
	Client                   *string                   `json:"Client,omitempty"`
	LastActivityDate         time.Time                 `json:"LastActivityDate"`
	LastPlaybackCheckIn      time.Time                 `json:"LastPlaybackCheckIn"`
	LastPausedDate           *time.Time                `json:"LastPausedDate,omitempty"`
	DeviceName               *string                   `json:"DeviceName,omitempty"`
	DeviceType               *string                   `json:"DeviceType,omitempty"`
	NowPlayingItem           *SDKBaseItemDto           `json:"NowPlayingItem,omitempty"`
	NowViewingItem           *SDKBaseItemDto           `json:"NowViewingItem,omitempty"`
	DeviceID                 *string                   `json:"DeviceId,omitempty"`
	ApplicationVersion       *string                   `json:"ApplicationVersion,omitempty"`
	TranscodingInfo          any                       `json:"TranscodingInfo,omitempty"`
	IsActive                 bool                      `json:"IsActive"`
	SupportsMediaControl     bool                      `json:"SupportsMediaControl"`
	SupportsRemoteControl    bool                      `json:"SupportsRemoteControl"`
	NowPlayingQueue          []any                     `json:"NowPlayingQueue,omitempty"`
	NowPlayingQueueFullItems []SDKBaseItemDto          `json:"NowPlayingQueueFullItems,omitempty"`
	HasCustomDeviceName      bool                      `json:"HasCustomDeviceName"`
	PlaylistItemID           *string                   `json:"PlaylistItemId,omitempty"`
	ServerID                 *string                   `json:"ServerId,omitempty"`
	UserPrimaryImageTag      *string                   `json:"UserPrimaryImageTag,omitempty"`
	SupportedCommands        []string                  `json:"SupportedCommands"`
}

// SDKDisplayPreferencesDto maps to org.jellyfin.sdk.model.api.DisplayPreferencesDto
type SDKDisplayPreferencesDto struct {
	ID                 *string            `json:"Id,omitempty"`
	ViewType           *string            `json:"ViewType,omitempty"`
	SortBy             *string            `json:"SortBy,omitempty"`
	IndexBy            *string            `json:"IndexBy,omitempty"`
	RememberIndexing   bool               `json:"RememberIndexing"`
	PrimaryImageHeight int                `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int                `json:"PrimaryImageWidth"`
	CustomPrefs        map[string]*string `json:"CustomPrefs"`
	ScrollDirection    SDKScrollDirection  `json:"ScrollDirection"`
	ShowBackdrop       bool               `json:"ShowBackdrop"`
	RememberSorting    bool               `json:"RememberSorting"`
	SortOrder          SDKSortOrder       `json:"SortOrder"`
	ShowSidebar        bool               `json:"ShowSidebar"`
	Client             *string            `json:"Client,omitempty"`
}

// SDKPublicSystemInfo maps to org.jellyfin.sdk.model.api.PublicSystemInfo
type SDKPublicSystemInfo struct {
	LocalAddress           *string `json:"LocalAddress,omitempty"`
	ServerName             *string `json:"ServerName,omitempty"`
	Version                *string `json:"Version,omitempty"`
	ProductName            *string `json:"ProductName,omitempty"`
	OperatingSystem        *string `json:"OperatingSystem,omitempty"`
	ID                     *string `json:"Id,omitempty"`
	StartupWizardCompleted *bool   `json:"StartupWizardCompleted,omitempty"`
}

// SDKBrandingOptions maps to org.jellyfin.sdk.model.api.BrandingOptions
type SDKBrandingOptions struct {
	LoginDisclaimer     *string `json:"LoginDisclaimer,omitempty"`
	CustomCSS           *string `json:"CustomCss,omitempty"`
	SplashscreenEnabled bool    `json:"SplashscreenEnabled"`
}
