# Phase 5: Session Manager Refactor

## Plans 21-25: Clean session management

### Plan 21: Simplify Session struct
- Remove OutputVideoCodec, OutputAudioCodec, OutputContainer from Session
- These belong on the ClientProfile/OutputPluginConfig, not the session
- Session owns: ID, ChannelID, StreamID, StreamURL, OutputDir, FanOut
- Session does NOT own pipeline internals (decoder, encoder, muxer)
- FanOut is the session's "output" — attach/detach plugins as needed

### Plan 22: Consumer model refactor
- Current: consumers are file-tailers (TailFile/TailReader)
- New: consumers are output plugins attached to the FanOut
- "Add viewer" = attach output plugin to FanOut
- "Remove viewer" = detach output plugin from FanOut
- "Add recording" = attach RecordingPlugin to FanOut
- Consumer count = len(FanOut.plugins)
- Zero consumers → session cleanup (same as before)

### Plan 23: Session keying review
- Currently keyed by channelID
- This works for the current model (one session per channel)
- For direct stream playback, channelID IS the streamID
- Document this explicitly — not a bug, just needs clarity
- Future: if multi-user needs separate sessions, key by channelID+userID

### Plan 24: Auto-recovery with output plugins
- Current: retry recreates the entire pipeline
- New: retry recreates DemuxSession + DecodeBridge
- FanOut persists across retries — output plugins keep their state
- RecordingPlugin continues recording across source reconnects
- MSE/HLS plugins reset (new generation) but stay alive

### Plan 25: Seek with output plugins
- Demuxer.RequestSeek → DecodeBridge.ResetForSeek → FanOut.ResetForSeek
- Each output plugin handles seek reset independently
- MSEPlugin: bump generation, new init segments
- HLSPlugin: reset muxer, new segments
- StreamPlugin: reset muxer
- RecordingPlugin: no-op (recording continues, seek is a playback concept)
