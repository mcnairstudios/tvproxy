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
- [ ] `StartTranscode()` still hardcodes `exec.CommandContext(rctx, "ffmpeg", args...)`
- [ ] Should build GStreamer pipeline with `OutputFormat: OutputHLS` when gstreamer.Available()
- [ ] Local duplicate helpers (`mapEncoderHW`, `isHTTPURL`, `isRTSP`, `isHEVC`) should be deleted, replaced with `media.*`

### Proxy transcode (pkg/service/proxy.go)
- [ ] `startFFmpeg()` should be renamed `startTranscoder()`
- [ ] Should build GStreamer pipeline when `gstreamer.Available()` and profile supports it
- [ ] Currently only handles ffmpeg-style profile.Args with `{input}` substitution

### VOD args composition (pkg/service/vod.go)
- [ ] `composeSessionArgs()` still calls `ffmpeg.Build(ffmpeg.BuildOptions{...})`
- [ ] Should use `gstreamer.BuildFromProbe()` when transcoder preference is gstreamer
- [ ] The `sessionArgs` struct has `Command: "ffmpeg"` as default

### VOD recording (pkg/service/vod_recording.go)
- [ ] Still calls `ffmpeg.Build()` for recording args
- [ ] Should use GStreamer pipeline for recording output

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
