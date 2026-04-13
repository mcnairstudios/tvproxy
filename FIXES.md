# Fixes & Ideas (Not on Critical Path)

## What's Working (feature/gstreamer-simplify)
- Unified pipeline builder: `gstreamer.Build()` with 2 paths (MPEG-TS native, VOD qtdemux)
- SAT>IP AV1 transcode: stable 30s+ runs, 4000+ kbps
- VOD H.265→AV1 transcode: sub-10s first byte, 5000+ kbps
- HDHR H.264 copy: 3.3s first byte
- VOD copy: 1.1s first byte
- All 13 test packages pass, 70+ gstreamer-specific test cases
- Dockerfile uses gavinmcnair/gstreamer:1.1 (GStreamer 1.24.12)
- NVENC, VAAPI LP, QSV, VideoToolbox encoder chains with fallbacks
- Probe-driven pipeline: ensureProbe() blocks if no cache, source profile as override
- User-friendly error messages, bitrate/file_size in status endpoint
- fragment-duration=2000ms (fixes "Could not multiplex" with svtav1enc)

## Remaining Issues (Priority Order)

## Tuner Contention
- SAT>IP copy test failed with 0 bytes when tuner was already locked by prior transcode test
- HDHR copy errored at 5.7s with "Internal data stream error" — likely tuner conflict
- Need proper tuner release between sequential tests (pipeline.SetState(StateNull) + sleep)
- In production: session manager should wait for tuner release before creating new session on same tuner

## Plugin Static Pad Transcode
- Attempted linking encode chain to tvproxydemux's static `video` pad — produced 0 bytes
- The static pad works for copy (plugin→plugin) but not for external encode chains
- Root cause: likely caps negotiation issue between plugin's internal parser and external decoder
- For now: transcode uses native tsdemux path (proven working)
- Future: tvproxydemux could expose a raw/decoded video pad option

## DONE: Plugin container-hint / audio-codec-hint
- container-hint, audio-codec-hint, video-codec-hint all implemented in tvproxydemux
- Built and tested locally — all three properties appear in gst-inspect
- Need to: rebuild gavinmcnair/gstreamer Docker image with updated plugin
- Need to: update builder.go to pass hints when using plugin path (currently deferred due to go-gst NewPipelineFromString issue)

## Stream Source Failover
- When primary source fails (IPTV connection refused, tuner busy), should try secondary source
- This is in the stream resolution layer (service/vod.go, service/proxy.go), not the pipeline builder
- Existing channel→stream mapping already supports multiple streams per channel
- Need: retry logic that picks next stream when first pipeline fails

## Active Stream Tracking
- Limited tuners per HDHR device, limited connections per IPTV provider
- Session manager tracks active sessions per channel (existing)
- Need to ensure tuner/connection count is checked BEFORE starting new pipeline
- Consider: per-source connection limits in M3U account settings

## Probe Cache Critical for Channel Switching Speed
- Without cached probe data, channel switching requires a live probe (2-5s delay)
- The probe cache MUST be pre-populated for all channels via the probe scheduler
- Channel settings (codec, audio, container) should come from probe cache, not live detection
- The probe scheduler already runs on startup — ensure it covers all channels with streams
- Consider: priority probe queue when user navigates to a channel without cached data

## Migrate proxy.go to Build()
- service/proxy.go still uses gst-launch-1.0 subprocess via BuildPipeline()
- Need Build() variant that outputs to fdsink/appsink instead of filesink
- Lower priority — proxy path works, VOD path was the broken one

## Migrate hls/session.go and jellyfin/playback.go to Build()
- Both still use old BuildPipeline() for string pipelines + gst-launch subprocess
- HLS output requires hlssink2/hlscmafsink elements — different from filesink
- Need a Build() variant that outputs to HLS dir instead of file
- Lower priority — Jellyfin HLS works via existing path, browser uses VOD stream

## avprobe FormatName Not Populated
- `ProbeResult.FormatName` is never set by avprobe package
- Causes container detection to fall back to URL extension matching
- Should extract format_name from ffprobe/libavformat during probe
- File: pkg/avprobe/avprobe.go — need to read `format_name` from AVFormatContext

## RTSP copy mode produces 0 bytes with mp4mux
- RTSP source (rtspsrc ! rtpmp2tdepay) with h264parse → mp4mux produces 0 bytes
- Same pipeline with HTTP source (souphttpsrc ! tsparse ! tsdemux) works fine (5.4MB)
- Root cause: RTSP RTP timestamps don't align with what mp4mux expects for copy mode
- Transcode works on RTSP because decode/encode re-timestamps
- Options:
  1. For RTSP copy: use mpegtsmux instead of mp4mux (native TS output)
  2. For RTSP copy: always transcode to at least re-timestamp (defeats purpose)
  3. For RTSP copy: use plugin path via gst-launch subprocess (plugins work)
- Affects: SAT>IP copy mode. Browser playback (always transcode) is NOT affected.

## RESOLVED: "Could not multiplex" on SAT>IP AV1 transcode
- Fixed by increasing mp4mux fragment-duration from 500ms to 2000ms
- Root cause: svtav1enc buffers 15+ seconds before first output, audio was 15s ahead of video
- mp4mux with 500ms fragments couldn't handle the timestamp gap
- 2000ms fragments give enough headroom for the encoder's startup buffering

## Proxy profile session creation returns 500
- Creating a VOD session with `?profile=Proxy` returns 500 internal server error
- The Proxy profile is meant for HTTP passthrough (no transcoding)
- May not be compatible with the VOD session flow (which expects a GStreamer pipeline)
- Direct/Proxy profiles should probably bypass the session manager entirely
- The correct path for Proxy: HTTP reverse proxy to the source URL

## Multiple simultaneous AV1 transcodes stall on M1
- Running 2 svtav1enc instances concurrently causes both to stall
- svtav1enc preset=12 uses all available CPU cores
- On the A380 with vaav1enc (hardware), this won't be an issue
- Consider: limit concurrent AV1 transcodes to 1 (or number of hw encoders)
- Consider: lower preset (higher number = faster but lower quality) for concurrent sessions

## ensureProbe doesn't use WireGuard for probing
- `ensureProbe()` calls `avprobe.Probe()` which uses libavformat directly (no custom HTTP client)
- WireGuard-routed sources would fail to probe (connection blocked without WG tunnel)
- The probe scheduler worker handles this correctly (uses configured HTTP client)
- `ensureProbe` is only for edge cases (never-probed channels)
- Fix: use the scheduler's probe method instead of direct avprobe

## Deinterlace not wired in GStreamer builder
- Source profile Deinterlace flag exists but not used in builder.go
- For GStreamer: insert `deinterlace` element after decoder in transcode chain
- Or: use `vavpp` (VAAPI) / `vtdec deinterlace=true` (VideoToolbox) for hardware deinterlace
- tvproxydemux detects interlaced content (video-interlaced property) — could auto-insert

## RestartWithSeek is ffmpeg-specific
- pkg/session/manager.go:485 — manipulates ffmpeg -ss args
- For GStreamer: either send seek event to pipeline, or create new pipeline with seek offset
- VOD seeking works differently in GStreamer — use gst_element_seek_simple()
- Or: for qtdemux, set souphttpsrc Range header for HTTP byte-range seeking

## go-gst NewPipelineFromString doesn't work with plugin bins
- `gst-launch-1.0` with tvproxysrc/tvproxydemux/tvproxymux produces output
- Same pipeline string via go-gst `NewPipelineFromString` produces 0 bytes
- Root cause: go-gst handles GstBin elements (our plugins) differently than gst-launch
- Workaround: always use native programmatic pipeline (buildMPEGTSNative)
- Future: investigate go-gst bin element handling, or use gst-launch subprocess for plugin path

## GLib-GObject-CRITICAL warnings
- `g_boxed_type_register_static: assertion 'g_type_from_name (name) == 0' failed`
- Appears on first plugin use — harmless but noisy
- Likely a type registration race in go-gst bindings
