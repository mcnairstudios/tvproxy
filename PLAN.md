# Future Work Plan

## Critical Fixes — DONE

All four critical fixes have been implemented in this session:

1. **timescale=0** — `PushAudioFrame` refuses audio, plays video-only, frontend shows "Missing Audio Probe"
2. **aacEncoder() nil** — Logs which encoders were tried, caught at pipeline build time
3. **pollFileProgress** — Uses real decode timing for MSE, file-size heuristic only for stream
4. **Live pipeline watchdog** — 30s no-activity timeout prevents zombie sessions

---

## tvproxy Work (this codebase)

### 5. Element Nil Checks Inconsistent (MEDIUM)
**File:** `pkg/gstreamer/builder.go`, `native.go`

Some elements have nil guards (`if di != nil`), others don't. `checkNilElements()` catches at the end but the error says "element N is nil" without identifying which plugin is missing.

**Fix:** Use `mustElement()` (already defined in native.go:35) consistently, or improve `checkNilElements()` error message to include element names.

### 6. Merge RunSegmentFlusher into PushVideoFrame
**File:** `pkg/fmp4/trackstore.go` lines 615-633

`RunSegmentFlusher` runs a ticker at 500ms to flush audio. But `PushVideoFrame` already triggers `FlushNow()` on the partner (audio) at keyframe + 2s boundary. The flusher is a redundant safety net.

**Fix:** Remove `RunSegmentFlusher`. If needed, add a fallback timer inside `PushAudioFrame` that flushes after 3s of no partner flush. Simplifies concurrency model.

### 7. Segment Memory Compaction
**File:** `pkg/fmp4/trackstore.go` lines 329-333

`maxSegments = 60` means 60 × ~200KB = 12MB per stream. Go slice trimming doesn't free underlying array memory immediately.

**Fix:** Copy trimmed segments to a new slice periodically, or use a ring buffer with fixed capacity.

### 8. AV1 OBU Parsing Hardening
**File:** `pkg/fmp4/av1.go`

`readLEB128()` returning `(0, 0)` on malformed data could cause infinite loops if size is 0 and loop doesn't advance. The `if obuEnd > len(data) break` guards are present but off-by-one errors possible.

**Fix:** Add explicit bounds checking: `if size == 0 && sizeLen == 0 { break }` and `if i >= len(data) { break }` at top of each loop.

### 9. Queue Size Tuning
Default `max-size-time` queues: 3s for appsink, 10s for file. For 4K content at 20Mbps, 10s = 25MB per queue. No backpressure handling if downstream is slow.

**Fix:** Make queue sizes configurable via source profile. Or use `max-size-bytes` alongside `max-size-time`.

### 10. Seek Improvements
Current seek uses `gst_element_seek_simple()` with hardcoded `FLUSH | KEY_UNIT` flags. For VOD seeking, `SNAP_BEFORE` might give more precise results. For live seeking (DVR), `SEGMENT` seeks would avoid pipeline flush.

### 11. go-gst Plugin Bin Issue
`buildMPEGTSPluginCopy()` is disabled because go-gst `NewPipelineFromString()` produces 0 bytes with plugin bins. This blocks the fastest copy path. The same pipeline works with `gst-launch-1.0` CLI.

**Investigation:** Likely a pad-added signal timing issue. The go-gst bindings may not process signals correctly when pipeline elements are bins (tvproxysrc, tvproxydemux are GstBin subclasses). Test with `gst.NewPipeline()` + manual element creation instead of string parsing.

---

## Plugin Work (handled by gstreamer-plugin agent)

**Full plan at:** `/Users/gavinmcnair/claude/gstreamer-plugin/PLAN.md`

This work is NOT done in the tvproxy codebase. The gstreamer-plugin agent handles:

- **tvproxydecode** — HW-aware decoder bin with automatic fallback chains
- **tvproxyencode** — HW-aware encoder bin with format converters and speed presets
- **tvproxyfmp4** — fMP4 segment producer replacing TrackStore (signal-based output)
- **tvproxydemux enhancements** — expose probe data as properties, add DTS/TrueHD/Opus/FLAC
- **tvproxymux enhancements** — AV1 container auto-switch, configurable fragment duration
- **tvproxyvod** — fix stall or deprecate in favor of souphttpsrc

Once the plugins land, tvproxy Go code will be simplified to consume them. That integration work WILL be done here, but only after the plugins are ready.

---

## Architecture Notes

### Three Active Playback Paths (Post-Probe-Wiring)
1. **MSE** (browser) — `session.Manager.runPipeline()` → `gstreamer.Build()` → native pipeline → TrackStore → HTTP segments. Full probe data.
2. **Stream** (DLNA/Jellyfin channel) — `proxy.startGStreamerProxy()` → probe cache + `BuildFromProbe()` → `gst-launch-1.0` subprocess. Full probe data.
3. **Stream** (VOD/recording) — `session.Manager.runPipeline()` → `gstreamer.Build()` → native pipeline → file → TailReader. Full probe data.

### Output Codec Selection
Output codec comes from **client stream profile** (Jellyfin, DLNA, Browser, Plex, etc.). The profile specifies H264, H265, AV1, or copy. Source stream profiles handle input-side differences (deinterlace, RTSP settings, queue sizes). Probe data drives auto-detection (deinterlace, bit depth, channel count).

### Plugin vs Native Decision
`PluginsAvailable()` checks for tvproxysrc+tvproxydemux+tvproxymux. If present AND copy mode, uses plugin string pipeline via `gst-launch-1.0`. Otherwise uses native Go element construction. The native path is dominant because: (a) transcode needs decoder/encoder elements not in plugins, (b) MSE needs appsink not in plugins, (c) go-gst string pipeline issue blocks plugin use from native Go code.
