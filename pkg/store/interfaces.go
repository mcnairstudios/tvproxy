package store

import (
	"context"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
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
	GetByID(ctx context.Context, id string) (*models.Stream, error)
	CountByAccountID(ctx context.Context, accountID string) (int, error)
	CountBySatIPSourceID(ctx context.Context, sourceID string) (int, error)
}

type StreamWriter interface {
	BulkUpsert(ctx context.Context, streams []models.Stream) error
	DeleteStaleByAccountID(ctx context.Context, accountID string, keepIDs []string) ([]string, error)
	DeleteByAccountID(ctx context.Context, accountID string) error
	DeleteStaleBySatIPSourceID(ctx context.Context, sourceID string, keepIDs []string) ([]string, error)
	DeleteBySatIPSourceID(ctx context.Context, sourceID string) error
	Delete(ctx context.Context, id string) error
	Clear() error
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
	ListNowPlaying(ctx context.Context, now time.Time) (map[string]string, error)
	ListForGuide(ctx context.Context, start, end time.Time) ([]models.GuideProgram, error)
	ListPrograms(ctx context.Context, epgDataID string) ([]models.ProgramData, error)
	ListProgramsByEPGDataIDs(ctx context.Context, ids []string) (map[string][]models.ProgramData, error)
}

type EPGWriter interface {
	BulkCreateEPGData(ctx context.Context, data []models.EPGData) error
	BulkCreatePrograms(ctx context.Context, programs []models.ProgramData) error
	DeleteBySourceID(ctx context.Context, sourceID string) error
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
	GetProbe(streamID string) (*ffmpeg.ProbeResult, error)
	SaveProbe(streamID string, result *ffmpeg.ProbeResult) error
}

type RecordingReader interface {
	List(userID string, isAdmin bool) ([]RecordingEntry, error)
	FilePath(streamID, filename string) (string, error)
	GetMeta(streamID, filename string) (*RecordingMeta, error)
}

type RecordingWriter interface {
	Save(streamID string, srcPath string, meta RecordingMeta) (string, error)
	Delete(streamID, filename string) error
}

type RecordingStore interface {
	ProbeCache
	RecordingReader
	RecordingWriter
}
