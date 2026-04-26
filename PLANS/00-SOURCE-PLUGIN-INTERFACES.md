# Source Plugin Interfaces

## Package: pkg/source/

### Core Interface

```go
type SourceType string

type SourceInfo struct {
    ID                  string
    Type                SourceType
    Name                string
    IsEnabled           bool
    StreamCount         int
    LastRefreshed       *time.Time
    LastError           string
    SourceProfileID     string
    MaxConcurrentStreams int
}

type Source interface {
    Info(ctx context.Context) SourceInfo
    Refresh(ctx context.Context) error
    Streams(ctx context.Context) ([]string, error)
    DeleteStreams(ctx context.Context) error
    Type() SourceType
}

type StreamSink interface {
    BulkUpsert(ctx context.Context, streams []models.Stream) error
    Save() error
}
```

### Status Reporting

```go
type StatusReporter interface {
    RefreshStatus(id string) RefreshStatus
}
```

### Optional Interfaces

```go
type Discoverable interface {
    Discover(ctx context.Context) ([]DiscoveredDevice, error)
}

type Retunable interface {
    Retune(ctx context.Context) error
}

type ConditionalRefresher interface {
    SupportsConditionalRefresh() bool
}

type VPNRoutable interface {
    UsesVPN() bool
}

type VODProvider interface {
    SupportsVOD() bool
    VODTypes() []string
}

type EPGProvider interface {
    ProvidesEPG() bool
}

type Clearable interface {
    Clear(ctx context.Context) error
}
```

### Registry

```go
type Factory func(ctx context.Context, sourceID string) (Source, error)

type Registry interface {
    Register(st SourceType, factory Factory)
    Get(ctx context.Context, sourceID string) (Source, error)
    ListAll(ctx context.Context) ([]SourceInfo, error)
    Types() []SourceType
}
```

### Stream Ownership Migration

Replace per-type foreign keys (M3UAccountID, SatIPSourceID, HDHRSourceID) with:
```go
SourceType string
SourceID   string
```

### Mapping to Current Code

| Current | Maps To | Optional Interfaces |
|---------|---------|-------------------|
| M3UService | Source | ConditionalRefresher, VPNRoutable, VODProvider (Xtream), Clearable |
| SatIPService | Source | Clearable |
| HDHRSourceService | Source | Discoverable, Retunable, Clearable |
