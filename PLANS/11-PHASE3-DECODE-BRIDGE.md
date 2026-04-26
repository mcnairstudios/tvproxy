# Phase 3: DecodeBridge — Extract decode/encode from pipelines

## Plans 11-14: Eliminate the duplicated decode/encode chains

### Plan 11: Create pkg/output/bridge.go — DecodeBridge
- Extract common decode/encode logic from MSETranscodePipeline
- DecodeBridge implements PacketSink
- Owns: videoDec, audioDec, audioResample, audioEnc, audioFifo, deint, scaler, videoEnc
- Receives compressed packets → decodes → processes → re-encodes → pushes to downstream
- Downstream is a PacketSink (FanOut or single OutputPlugin)
- All PTS handling stays exactly as-is in current code
- ResetForSeek: flush decoders + resampler + audioFifo + encoders
- Tests: feed test packets through bridge, verify output
- **New code only initially**

### Plan 12: Wire DecodeBridge into session manager
- Replace the 6 runGoXxx methods with one runPipeline
- Copy mode: Demuxer → FanOut → [plugins]
- Transcode mode: Demuxer → DecodeBridge → FanOut → [plugins]
- The delivery plugin (MSE/HLS/Stream) no longer owns decode/encode
- It just receives encoded packets via PushVideo/PushAudio
- Seek wiring: demuxer.SetOnSeek → bridge.ResetForSeek + fanOut.ResetForSeek

### Plan 13: Delete old pipeline types from gopipeline.go
- MSECopyPipeline → replaced by MSEPlugin (no bridge)
- MSETranscodePipeline → replaced by MSEPlugin (with bridge)
- HLSCopyPipeline → replaced by HLSPlugin (no bridge)
- HLSTranscodePipeline → replaced by HLSPlugin (with bridge)
- StreamCopyPipeline → replaced by StreamPlugin (no bridge)
- FullTranscodePipeline → replaced by StreamPlugin (with bridge)
- AudioTranscodePipeline → audio decode chain is in DecodeBridge
- gopipeline.go shrinks from ~2400 lines to ~200 (just the pipeline opts structs if still needed)

### Plan 14: Verify all delivery modes still work
- Test MSE copy mode (IPTV h264 → browser)
- Test MSE transcode mode (IPTV → av1 on A380)
- Test HLS copy mode (HDHR → browser)
- Test HLS transcode mode (SAT>IP → browser)
- Test stream copy mode (DLNA, Jellyfin)
- Test recording (always-on + manual record)
- Test seeking (VOD)
- Test auto-recovery (live stream failure + retry)
