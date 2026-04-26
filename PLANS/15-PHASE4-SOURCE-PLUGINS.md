# Phase 4: Source Plugin Migration

## Plans 15-20: Wrap existing services as Source plugins

### Plan 15: Implement Source interface on M3UService
- M3UService already has all the methods, just different signatures
- Create adapter in pkg/source/m3u/ or modify M3UService directly
- Info() → reads M3UAccountStore
- Refresh() → wraps refreshAccount
- DeleteStreams() → wraps DeleteByAccountID
- Implement: ConditionalRefresher, VPNRoutable, VODProvider (Xtream), Clearable
- Register factory with source.Registry

### Plan 16: Implement Source interface on HDHRSourceService
- Info() → reads HDHRSourceStore
- Refresh() → wraps ScanSource
- Implement: Discoverable (UDP broadcast), Retunable, Clearable
- Auto-link devices on creation (already done)
- Register factory

### Plan 17: Implement Source interface on SatIPService
- Info() → reads SatIPSourceStore
- Refresh() → wraps scanSource
- Implement: Clearable
- Register factory

### Plan 18: Add SourceType + SourceID to models.Stream
- Add fields alongside existing M3UAccountID/SatIPSourceID/HDHRSourceID
- Populate during refresh in each source plugin
- Backward compatible — old fields still work
- Migration: on startup, fill SourceType/SourceID from existing foreign keys

### Plan 19: Unified /api/sources endpoint
- New handler that calls Registry.ListAll()
- Returns all sources across all types in one list
- Frontend can show unified source management page
- Per-type endpoints still work for type-specific operations

### Plan 20: Collapse stream store methods
- Replace ListByAccountID, ListBySatIPSourceID, ListByHDHRSourceID
- With ListBySource(ctx, sourceType, sourceID)
- Same for DeleteBy*, DeleteStaleBy*
- Keep old methods as wrappers during migration
