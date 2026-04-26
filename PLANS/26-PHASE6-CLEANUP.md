# Phase 6: Cleanup and Polish

## Plans 26-30: Remove dead code, update docs, fix remaining issues

### Plan 26: Delete gopipeline.go pipeline types
- After all output plugins are wired and tested
- Delete: MSECopyPipeline, MSETranscodePipeline, HLSCopyPipeline, HLSTranscodePipeline
- Delete: StreamCopyPipeline, FullTranscodePipeline, AudioTranscodePipeline
- gopipeline.go either deleted entirely or reduced to shared helpers
- Verify build + all tests pass

### Plan 27: Delete old pkg/hls/ subprocess manager
- Dead code since libavformat HLS muxer replaced it
- Remove pkg/hls/ entirely
- Remove references from Jellyfin playback.go if still present
- Update CLAUDE.md

### Plan 28: Clean up service layer
- VODService no longer manages pipeline internals
- ProxyService delegates to session manager + output plugins
- Remove codec/container logic scattered across service layer
- Strategy layer remains (determines copy vs transcode)
- Service → session manager → output plugins (clean chain)

### Plan 29: Update CLAUDE.md with new architecture
- Document pkg/source/ plugin system
- Document pkg/output/ plugin system
- Document FanOut + DecodeBridge architecture
- Document recording flow (always-on + manual preserve)
- Remove references to old pipeline types
- Update "Pipeline Paths" section
- Update "Key Files" section

### Plan 30: Update DESIGN.md with implementation status
- Mark completed phases
- Document any design changes made during implementation
- Note remaining work (WebRTC, DASH, series recording, timeshift)
