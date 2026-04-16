# Fixes & Ideas (Not on Critical Path)

## How to Test

```bash
# Build
CGO_ENABLED=1 go build -tags enable_gstreamer -o ./tvproxy ./cmd/tvproxy/

# Run (with plugins)
GST_PLUGIN_PATH=/Users/gavinmcnair/claude/gstreamer-plugin/builddir:/Users/gavinmcnair/claude/tvproxymux/build:/Users/gavinmcnair/claude/tvproxysrc/build \
TVPROXY_USER_AGENT="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36" \
TVPROXY_RECORD_DIR=/tmp/recordings \
TVPROXY_VOD_OUTPUT_DIR=/tmp/recordings \
TVPROXY_BASE_URL=http://192.168.0.111 \
./tvproxy
```

## High Priority

### 1. Audio-Master Segment Cutting (A/V Sync Drift)
**PARTIALLY RESOLVED:** PTS-driven timeline with shared base PTS eliminates drift from accumulated durations. Both tracks derive decodeTime directly from GStreamer PTS via shared atomic base. Rolling average handles erratic durations.

**Remaining risk:** If audio PTS and video PTS diverge significantly (>100ms), segments may have A/V offset. Monitor in long (1hr+) sessions on A380.

**Full fix (if needed):** Audio PTS becomes master clock for segment boundaries — video cuts aligned to audio boundaries.

### 2. Tuner Contention
SAT>IP copy test failed with 0 bytes when tuner was locked by prior session. HDHR errored with "Internal data stream error" on tuner conflict.

**Fix:** Session manager checks tuner availability before creating new pipeline.
- `pipeline.SetState(StateNull)` must complete synchronously before removing session
- Wait for RTSP TEARDOWN before starting new session on same tuner
- Per-source connection limits in M3U account settings

### 3. Auto-Recovery for Live Stream Pipeline Drops
When live pipeline EOS's (DVB signal drop) or errors, should auto-restart.

**Fix:**
- Keep same session/consumers, rebuild GStreamer pipeline
- Retry count + backoff (max 3 retries, 2s between)
- Frontend MSE handles reconnect via generation counter (already implemented)

### 4. RTSP Copy Mode Produces 0 Bytes
RTSP source with h264parse → mp4mux produces 0 bytes. Transcode works (re-timestamps). HTTP source copy works.

**Root cause:** RTSP RTP timestamps don't align with mp4mux expectations.

**Options:**
1. RTSP copy → use mpegtsmux instead of mp4mux
2. RTSP copy → always transcode (defeats purpose)
3. RTSP copy → use plugin path via gst-launch subprocess

**Impact:** SAT>IP copy mode only. Browser playback (always transcode) NOT affected.

### 5. ensureProbe Doesn't Use WireGuard
`ensureProbe()` uses libavformat directly (no custom HTTP client). WireGuard-routed sources fail to probe.

**Fix:** Use the probe scheduler's method instead of direct avprobe. The scheduler already handles WG routing.

### 6. ForceSeedClientDefaults Wipes User Customizations
`ForceSeedClientDefaults` (called on every startup in main.go) clears all clients and re-seeds. Any user edits to client profiles are lost on restart. `SeedClientDefaults` (skip-if-non-empty) is only used internally now.

**Fix:** main.go should call `SeedClientDefaults` (non-force). Only `HardReset` should call `ForceSeedClientDefaults`.

### 7. Hardcoded 16:9 Aspect Ratio for Output Scaling
`builder.go` calculates output width as `OutputHeight * 16 / 9`. Non-16:9 content (4:3 SD, 21:9 movies) will be stretched/squashed.

**Fix:** Use source aspect ratio from probe data (`SourceWidth / SourceHeight`) when available. Fall back to 16:9 only when probe is missing.

## Medium Priority

### 8. Migrate proxy.go to Native Build()
`service/proxy.go` still uses `gst-launch-1.0` subprocess via `BuildPipeline()`. Need `Build()` variant that outputs to `fdsink`/`appsink` instead of `filesink`.

### 9. Migrate Jellyfin HLS to Native Build()
`hls/session.go` and `jellyfin/playback.go` use old `BuildPipeline()` for string pipelines. HLS output needs `hlssink2`/`hlscmafsink` elements. Works today via gst-launch subprocess.

### 10. Stream Source Failover
When primary source fails, try secondary source. Channel→stream mapping already supports multiple streams. Need retry logic in `service/vod.go` and `service/proxy.go`.

### 11. Active Stream Tracking / Connection Limits
Limited tuners per HDHR, limited connections per IPTV provider. Need tuner/connection count check BEFORE starting new pipeline. Per-source connection limits in M3U account settings.

### 12. Probe Cache Pre-Population
Without cached probe, channel switching requires live probe (2-5s delay). Probe scheduler runs on startup but must cover all channels. Consider priority probe queue for uncached channels.

### 13. Duplicated Builder Logic
`buildMPEGTSNative` and `buildNonMPEGTSNative` share identical code for:
- 10-bit/VT HEVC decode fallback (`decHW` logic)
- Output scaling (`videoscale + videoconvert + capsfilter`)

Extract to shared helpers to reduce copy-paste drift risk.

### 14. GetInit/GetSegment Goroutine Churn
`TrackStore.GetInit()` and `GetSegment()` spawn a goroutine every 500ms to broadcast on the cond var (workaround for missed signals). Over a 30s timeout that's ~60 short-lived goroutines. Replace with `time.AfterFunc` + cancel on return.

## Low Priority / Future

### 15. WebRTC Sink for Sub-Second Latency
`webrtcsink` available in gstreamer:1.2. Requires signalling server (WebSocket), ICE/STUN/TURN. Transformative for live TV channel surfing.

### 16. WebM Output Needs Opus Audio
WebM container requires Opus, not AAC. `buildAudioChain` always outputs AAC. Low priority — WebM uncommon for IPTV/VOD.

### 17. Plugin Static Pad Transcode
tvproxydemux static `video` pad works for copy but not external encode chains. Caps negotiation issue. Native tsdemux path used instead (proven working).

### 18. Audio Channels on Client Stream Profile
Add `audio_channels` field (0=passthrough, 2=stereo, 6=5.1). Defaults: Browser=2, Phone=2, Plex=0, DLNA=2. Wire into buildAudioChain capsfilter.

### 19. Audio Language Selection in Native MPEG-TS Path
Native tsdemux takes first audio pad. Need to collect all audio pads, check language tags, select preferred. tvproxydemux does this internally — replicate in native path.

## Reference

### Codec → GStreamer Elements
| Codec | Parser | Decoder (VT/VA/NV/SW) | Encoder (VT/VA/NV/SW) |
|-------|--------|----------------------|----------------------|
| h264 | h264parse | vtdec / vah264dec / nvh264dec / avdec_h264 | vtenc_h264 / vah264lpenc / nvh264enc / x264enc |
| h265 | h265parse | vtdec / vah265dec / nvh265dec / avdec_h265 | vtenc_h265 / vah265lpenc / nvh265enc / x265enc |
| av1 | av1parse | dav1ddec / vaav1dec / nvav1dec / avdec_av1 | vtenc_av1 / vaav1lpenc / nvav1enc / svtav1enc |
| mpeg2 | mpegvideoparse | avdec_mpeg2video / vampeg2dec | — (always transcode) |

### Audio Chains
| Codec | Chain |
|-------|-------|
| aac_latm | aacparse → avdec_aac_latm → audioconvert → audioresample → faac → aacparse |
| aac | aacparse (passthrough in VOD, transcode in live) |
| mp2 | mpegaudioparse → mpg123audiodec → audioconvert → audioresample → faac → aacparse |
| ac3 | avdec_ac3 → audioconvert → audioresample → faac → aacparse |
| eac3 | avdec_eac3 → audioconvert → audioresample → faac → aacparse |
| dts | avdec_dca → audioconvert → audioresample → faac → aacparse |
| truehd | avdec_truehd → audioconvert → audioresample → faac → aacparse |
| flac | flacparse → flacdec → audioconvert → audioresample → faac → aacparse |
| opus | opusdec → audioconvert → audioresample → faac → aacparse |

### Hardware Acceleration
| Hardware | Setting | Encoders |
|----------|---------|----------|
| Intel Arc (A380) | `vaapi` | vaav1lpenc, vah265lpenc, vah264lpenc |
| Intel Gen9+ | `vaapi` | vah265lpenc, vah264lpenc |
| NVIDIA Turing+ | `nvenc` | nvav1enc, nvh265enc, nvh264enc |
| Apple Silicon | `videotoolbox` | vtenc_h265, vtenc_h264, vtenc_av1 (M4+) |
| Software | `none` | svtav1enc, x265enc, x264enc |

### MSE Architecture (Current)
```
GStreamer pipeline → appsink → Go TrackStore (fMP4 via mp4ff) → HTTP segments → Browser MSE
Keyframe detection: BufferFlagDeltaUnit (GStreamer flag, not OBU/NALU parsing)
Duration: rolling 10-frame average, PTS diff primary, buf.Duration fallback
AV1: strip TD OBUs, skip ≤10 byte buffers, sequence header in av1C configOBUs
Audio: always transcode to AAC for live, copy AAC for VOD
Seeking: CGO gst_element_seek_simple, generation counter invalidates old segments
```
