package store

import (
	"context"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type Versioned interface {
	ETag() string
}

type StreamReader interface {
	List(ctx context.Context) ([]models.Stream, error)
	ListSummaries(ctx context.Context) ([]models.StreamSummary, error)
	ListByAccountID(ctx context.Context, accountID string) ([]models.Stream, error)
	ListBySatIPSourceID(ctx context.Context, sourceID string) ([]models.Stream, error)
	ListByHDHRSourceID(ctx context.Context, sourceID string) ([]models.Stream, error)
	ListByVODType(ctx context.Context, vodType string) ([]models.Stream, error)
	GetByID(ctx context.Context, id string) (*models.Stream, error)
}

type StreamWriter interface {
	BulkUpsert(ctx context.Context, streams []models.Stream) error
	DeleteStaleByAccountID(ctx context.Context, accountID string, keepIDs []string) ([]string, error)
	DeleteByAccountID(ctx context.Context, accountID string) error
	DeleteStaleBySatIPSourceID(ctx context.Context, sourceID string, keepIDs []string) ([]string, error)
	DeleteBySatIPSourceID(ctx context.Context, sourceID string) error
	DeleteOrphanedM3UStreams(ctx context.Context, knownAccountIDs []string) ([]string, error)
	DeleteOrphanedSatIPStreams(ctx context.Context, knownSourceIDs []string) ([]string, error)
	DeleteStaleByHDHRSourceID(ctx context.Context, sourceID string, keepIDs []string) ([]string, error)
	DeleteByHDHRSourceID(ctx context.Context, sourceID string) error
	DeleteOrphanedHDHRStreams(ctx context.Context, knownSourceIDs []string) ([]string, error)
	Delete(ctx context.Context, id string) error
	UpdateTMDBID(ctx context.Context, id string, tmdbID int) error
	SetTMDBManual(ctx context.Context, id string, tmdbID int) error
	ClearAutoTMDBByAccountID(ctx context.Context, accountID string) error
	UpdateWireGuardByAccountID(ctx context.Context, accountID string, useWireGuard bool) error
	UpdateStreamProbeData(ctx context.Context, id string, duration float64, vcodec, acodec string) error
	Clear() error
}

type StreamBrowser interface {
	ListGroups(sourceKey string) ([]GroupInfo, error)
	ListByGroup(sourceKey, group string, offset, limit int) ([]models.StreamSummary, int, error)
	SearchByName(query string, limit int) ([]models.StreamSummary, error)
	StreamCount() int
}

type StreamPersister interface {
	Save() error
	Load() error
}

type StreamStore interface {
	StreamReader
	StreamWriter
	StreamPersister
}

type EPGReader interface {
	ListEPGData(ctx context.Context) ([]models.EPGData, error)
	ListBySourceID(ctx context.Context, sourceID string) ([]models.EPGData, error)
	GetNowByChannelID(ctx context.Context, channelID string, now time.Time) (*models.ProgramData, error)
	GetIconByChannelID(ctx context.Context, channelID string) string
	ListNowPlaying(ctx context.Context, now time.Time) (map[string]string, error)
	ListNowPlayingFull(ctx context.Context, now time.Time) (map[string]map[string]any, error)
	ListForGuide(ctx context.Context, start, end time.Time) ([]models.GuideProgram, error)
	ListPrograms(ctx context.Context, epgDataID string) ([]models.ProgramData, error)
	ListProgramsByEPGDataIDs(ctx context.Context, ids []string) (map[string][]models.ProgramData, error)
}

type EPGWriter interface {
	BulkCreateEPGData(ctx context.Context, data []models.EPGData) error
	BulkCreatePrograms(ctx context.Context, programs []models.ProgramData) error
	DeleteBySourceID(ctx context.Context, sourceID string) error
	DeleteOrphanedEPGData(ctx context.Context, knownSourceIDs []string) (int, error)
	DeleteProgramsByEPGDataID(ctx context.Context, epgDataID string) error
	Clear() error
}

type EPGPersister interface {
	Save() error
	Load() error
}

type EPGStore interface {
	EPGReader
	EPGWriter
	EPGPersister
}

type ProbeCache interface {
	GetProbe(streamID string) (*media.ProbeResult, error)
	SaveProbe(streamID string, result *media.ProbeResult) error
	DeleteProbe(streamID string) error
	SaveTSHeader(streamID string, header []byte) error
	GetTSHeader(streamID string) ([]byte, error)
}

type RecordingReader interface {
	List(userID string, isAdmin bool) ([]RecordingEntry, error)
	FilePath(streamID, filename string) (string, error)
	GetMeta(streamID, filename string) (*RecordingMeta, error)
}

type RecordingWriter interface {
	Delete(streamID, filename string) error
}

type SessionMetaStore interface {
	WriteSessionMeta(streamID string, meta SessionMeta) error
	ReadSessionMeta(streamID string) (*SessionMeta, error)
	RemoveActiveSession(streamID string) error
	CompleteRecording(streamID string, meta SessionMeta) (string, error)
	ListActiveRecordings() ([]SessionMeta, error)
	ActiveDir(streamID string) string
}

type RecordingStore interface {
	ProbeCache
	RecordingReader
	RecordingWriter
	SessionMetaStore
}
