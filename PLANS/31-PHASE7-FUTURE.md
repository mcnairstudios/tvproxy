# Phase 7: Future Work (not in overnight session)

## Plans 31-40: Next priorities after modular refactor

### Plan 31: WebRTC output plugin
- pkg/output/webrtc/webrtc.go
- Uses Pion WebRTC (pure Go)
- Direct frame fan-out from DecodeBridge — lowest latency
- Signalling via WebSocket (stub already exists in Jellyfin)
- Sub-second live TV latency (proven in GStreamer era)

### Plan 32: DASH output plugin
- pkg/output/dash/dash.go
- DASH manifest + fMP4 segments
- Similar to MSE but with manifest instead of JS worker

### Plan 33: Timeshift / pause live TV
- Leverage always-recording: pause = stop consuming, resume from offset
- Frontend: pause button seeks back into recording buffer
- Backend: RecordingPlugin is always running, viewer seeks within it

### Plan 34: Series recording rules
- "Record every episode of X" from EPG
- Match by series name + EPG channel
- Auto-schedule when EPG data refreshes
- Store rules in bolt, evaluate on EPG update

### Plan 35: Recording library management
- Browse recordings with metadata, poster images
- Delete, rename, move recordings
- Search by title, date, channel
- Jellyfin integration (show recordings as library items)

### Plan 36: Disk space management
- Monitor recording disk usage
- Auto-delete oldest temp recordings when space is low
- Configurable retention policies
- Warning when disk is running low

### Plan 37: Split frontend — admin UI vs media player
- Admin on /admin/ path
- Media player on / path (or separate app)
- Same API, different UIs
- Media player could be React/Vue/native app

### Plan 38: Unified source management UI
- Single page showing all sources (M3U, SAT>IP, HDHR, future)
- Source plugin registry drives the UI
- Add source → pick type → type-specific config form

### Plan 39: Multi-user session sharing
- Two users watch same channel → one decode, shared FanOut
- Each user has their own output plugin in the FanOut
- User A presses record → RecordingPlugin added for user A
- User B's playback unaffected

### Plan 40: Hardware capability auto-config
- Probe all HW platforms at startup
- Auto-set max bit depth (already done for VAAPI)
- Auto-detect supported codecs per platform
- Capabilities page shows detected vs configured
- Settings auto-populated from detection (override available)
