# Refactor TODO

Shortcuts taken during the GStreamer + libavformat integration. Each item should be completed in a dedicated refactoring session.

## Phase 3: Import migration (mechanical, ~22 files)

All files still import `pkg/ffmpeg` via the compat layer. Each should be migrated to import `pkg/media` (for types) and `pkg/avprobe` (for probing) directly.

### Store layer
- [ ] `pkg/store/interfaces.go` — `ffmpeg.ProbeResult` → `media.ProbeResult` (7 references)
- [ ] `pkg/store/probe_bolt.go` — same
- [ ] `pkg/store/recording.go` — same

### Session layer
- [ ] `pkg/session/session.go` — `ffmpeg.VideoInfo`, `ffmpeg.AudioTrack` → `media.*`
- [ ] `pkg/session/manager.go` — ~30 references: `ffmpeg.StreamHash`, `ffmpeg.IsHTTPURL`, `ffmpeg.Probe`, `ffmpeg.ProbeReader`, `ffmpeg.ShellSplit`, `ffmpeg.MapEncoderHW`, `ffmpeg.SanitizeFilename`, `ffmpeg.IsFFmpegNoise`

### Service layer
- [ ] `pkg/service/vod.go` — `ffmpeg.Build()`, `ffmpeg.StreamHash()`, `ffmpeg.Probe()`
- [ ] `pkg/service/vod_probe.go` — `ffmpeg.Probe()`, `ffmpeg.NormalizeVideoCodec()`, `ffmpeg.NormalizeContainer()`
- [ ] `pkg/service/vod_recording.go` — `ffmpeg.Build()`
- [ ] `pkg/service/proxy.go` — `ffmpeg.IsHTTPURL()`, `ffmpeg.ShellSplit()`, `ffmpeg.IsFFmpegNoise()`
- [ ] `pkg/service/m3u.go` — `ffmpeg.ProbeResult`, `ffmpeg.StreamHash()`
- [ ] `pkg/service/satip.go` — `ffmpeg.ProbeResult`
- [ ] `pkg/service/hdhr_source.go` — `ffmpeg.CaptureTPSHeader()`, `ffmpeg.StreamHash()`
- [ ] `pkg/service/strategy.go` — `ffmpeg.NormalizeVideoCodec()`

### GStreamer layer
- [ ] `pkg/gstreamer/pipeline.go` — `ffmpeg.ProbeResult` → `media.ProbeResult`
- [ ] `pkg/gstreamer/native.go` — same
- [ ] `pkg/gstreamer/transcoder.go` — `ffmpeg.StreamHash()` → `media.StreamHash()`
- [ ] `pkg/gstreamer/prober.go` — `ffmpeg.Probe()` → `avprobe.Probe()`, `ffmpeg.StreamHash()` → `media.StreamHash()`, `ffmpeg.CaptureTPSHeader()` → `media.CaptureTPSHeader()`

### Handler layer
- [ ] `pkg/handler/stream_profile.go` — `ffmpeg.DefaultContainer`
- [ ] `pkg/handler/integration_test.go` — test references

### Other
- [ ] `pkg/jellyfin/playback.go` — already has gstreamer import, verify media types
- [ ] `cmd/tvproxy/main.go` — `ffmpeg.SetSettings()`
- [ ] `pkg/database/migrations.go` — `ffmpeg.Build()`

## Phase 4 shortcuts: GStreamer not wired into all paths

### HLS session (pkg/hls/session.go)
- [x] `StartTranscode()` now uses GStreamer hlssink3 when gstreamer.Available()
- [x] Falls back to ffmpeg when GStreamer unavailable
- [ ] Local duplicate helpers (`mapEncoderHW`, `isHTTPURL`, `isRTSP`, `isHEVC`) should be deleted, replaced with `media.*`

### Proxy transcode (pkg/service/proxy.go)
- [x] `startFFmpeg()` now delegates to `startGStreamerProxy()` when gstreamer.Available()
- [ ] `startFFmpeg()` should be renamed `startTranscoder()`
- [ ] ffmpeg fallback still uses profile.Args with `{input}` substitution

### VOD file transcode (pkg/service/vod_probe.go)
- [x] `TranscodeFile()` now uses GStreamer BuildFromProbe when available
- [x] Falls back to ffmpeg when GStreamer unavailable

### VOD args composition (pkg/service/vod.go)
- [x] `composeSessionArgs()` already returns `gst-launch-1.0` command
- [x] Session manager's `resolveTranscoder` handles GStreamer pipeline building
- [ ] Fallback paths still return `Command: "ffmpeg"` (only used when GStreamer unavailable)

### VOD recording (pkg/service/vod_recording.go)
- [x] Recording goes through session manager which uses GStreamer via `resolveTranscoder`

### Jellyfin playback (pkg/jellyfin/playback.go)
- [x] Uses GStreamer for non-seek playback
- [ ] Seek (StartTimeTicks) still falls back to ffmpeg — GStreamer souphttpsrc doesn't support -ss

### Database migrations (pkg/database/migrations.go)
- [ ] Migration code calls `ffmpeg.Build()` to seed default profile args
- [ ] Should generate GStreamer-compatible defaults or remove ffmpeg dependency

## Phase 5: Delete pkg/ffmpeg/

After all Phase 3 + Phase 4 items are done:
- [ ] Delete `pkg/ffmpeg/compat.go`
- [ ] Delete `pkg/ffmpeg/probe.go` (ffprobe functions replaced by avprobe)
- [ ] Delete `pkg/ffmpeg/args.go` (empty file)
- [ ] Delete `pkg/ffmpeg/build.go`
- [ ] Delete `pkg/ffmpeg/compose.go`
- [ ] Delete `pkg/ffmpeg/autodetect.go`
- [ ] Delete `pkg/ffmpeg/compose_test.go`
- [ ] Remove `ffmpeg.SetSettings()` call from `cmd/tvproxy/main.go`

## Settings architecture
- [ ] `pkg/defaults/settings.json` has all settings under `"ffmpeg"` key
- [ ] Should be renamed to `"transcoder"` or similar
- [ ] GStreamer element properties (bitrate, tune, etc.) need a config path
- [ ] Encoder presets currently ffmpeg-specific (libx264 preset, vaapi quality)

## Frontend: auto-refresh after scan/refresh
- [ ] HDHR scan completion should trigger `rebuildStreamNav()` to update sidebar streams
- [ ] M3U refresh completion should do the same
- [ ] SAT>IP scan completion should do the same
- [ ] The scan progress polling already knows when state=done — add nav rebuild there

## CRITICAL: Live channel HLS output broken
- [ ] GStreamer session uses OutputMPEGTS but browser expects HLS at /vod/{id}/hls/playlist.m3u8
- [ ] For browser playback (delivery=hls), GStreamer pipeline needs OutputFormat=OutputHLS with hlssink3
- [ ] The session manager's resolveTranscoder sets OutputMPEGTS — needs to check if HLS is required
- [ ] HDHR streams need: souphttpsrc → tsdemux → h264parse → hlssink3 (not mpegtsmux → filesink)

## Strategy layer not wired to GStreamer
- [ ] `resolveVideoAction`/`resolveAudioAction` from strategy.go not passed to PipelineOpts
- [ ] GStreamer pipeline always uses global defaults instead of copy-when-codecs-match
- [ ] Need: if source codec == output codec → OutputVideoCodec = "copy"
- [ ] Need: audio codec compatibility check (AAC-LATM needs transcode, plain AAC can copy)

## Proactive probing of all sources
- [ ] HDHR scan should trigger avprobe.Probe() on each channel sequentially (uses idle tuner)
- [ ] 173 channels × 102ms avg = ~18 seconds total probe time
- [ ] Store results by BOTH stream hash AND stream ID in BBolt
- [ ] Same for SAT>IP channels after scan
- [ ] IPTV streams probed during M3U refresh (one at a time, respect connection limits)
- [ ] No hacks — everything probed properly, GStreamer always has data

## CustomPipeline override
- [ ] Add `CustomPipeline string` to `gstreamer.PipelineOpts`
- [ ] If set, `BuildPipeline()` returns that string directly
- [ ] Expose in StreamProfile UI for power users

## Mixed hardware acceleration
- [ ] `PipelineOpts.HWAccel` applies to both decode AND encode
- [ ] Need separate `DecodeHWAccel` and `EncodeHWAccel` fields
- [ ] Use case: VAAPI decode + software encode (or vice versa)

## Deinterlace integration
- [ ] `SourceProfile.Deinterlace` exists but is NOT used by GStreamer pipeline builder
- [ ] Should add deinterlace element to pipeline when enabled
- [ ] GStreamer: `deinterlace` (software) or `vaapideinterlace` (hardware)
