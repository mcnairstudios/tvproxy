# Implementation Plan — Passive Metadata Store

## 2-Hour Schedule

### Step 1 (0-30 min): Remove probe as pipeline dependency
- Remove `ensureProbe()` call from `runPipeline()`
- Remove `probeAsync()` goroutine from `GetOrCreateWithConsumer()`
- Pipeline uses cached probe data from session (already set from bolt cache read in GetOrCreateWithConsumer)
- If no cached data, pipeline builds with whatever GStreamer auto-detects
- Build, test: play a SAT>IP channel, play an IPTV channel, play a VOD file
- Verify: no `ensureProbe` or `probeAsync` in logs, playback still works

### Step 2 (30-60 min): Remove probe scheduler and workers
- Remove `pkg/gstreamer/prober.go`
- Remove `pkg/worker/probe.go`
- Remove probe scheduler creation from `cmd/tvproxy/main.go`
- Remove `ensureProbeEntries()` from M3U service
- Keep bolt probe cache store (passive reads still needed)
- Build, test: startup has no "probe jobs queued" log, no 404 probe spam

### Step 3 (60-90 min): Remove avprobe dependency
- Remove `pkg/avprobe/avprobe.go` (C libavformat probe — eliminates CGO dependency on libav)
- Remove `pkg/avprobe/probe_reader.go` (ffprobe subprocess)
- Update all callers: proxy.go live probe → use cached data only
- Keep `ProbeFile`/`ProbeStream` VOD handlers for now (UI probe-on-demand for single streams)
- Actually: VOD handlers can use the passive cache — if empty, return what we have
- Build, test: no avprobe references remain, playback unaffected

### Step 4 (90-120 min): Write-back from playback + cleanup
- After pipeline PLAYING, write session probe info back to passive cache
- This populates the cache for future UI display
- Remove unused imports, dead code from the removal
- Full test suite: `go test ./pkg/gstreamer/... ./pkg/fmp4/... ./pkg/handler/...`
- Play SAT>IP, IPTV, VOD — all working
- Verify passive cache populated after playback
- Final build, vet, smoke test

---

#

## Passive Metadata Store (replaces probe scheduler)

### What Changes

Remove the active probe scheduler entirely. Replace with a passive metadata cache that gets populated from three sources:

1. **Source metadata** (immediate, at import time):
   - tvproxy-streams: codec, resolution, audio info from M3U tags
   - Xtream: codec, resolution from API response
   - M3U IPTV: group, logo, name from M3U tags
   - HDHR: channel name from lineup
   - SAT>IP: channel name from scan

2. **Playback metadata** (after first play):
   - Read tvproxydemux properties after pipeline PLAYING + first data flows
   - video-codec, video-width, video-height, video-interlaced, video-bitrate
   - audio-source-codec, audio-sample-rate, audio-source-channels
   - Write to passive cache keyed by stream ID
   - Available for UI display on subsequent visits

3. **EPG metadata** (from XMLTV):
   - Programme-level format info (HD/SD, aspect ratio, audio format)
   - Already captured, no change needed

### What Gets Deleted

- `pkg/gstreamer/prober.go` — probe scheduler, probe workers, probe queue
- `pkg/avprobe/avprobe.go` — C libavformat probe (CGO dependency)
- `pkg/avprobe/probe_reader.go` — ffprobe subprocess probe
- `ensureProbe()` in session/manager.go — blocking pre-pipeline probe
- `probeAsync()` in session/manager.go — background probe on session start
- `probeOutputFile()` in session/manager.go — output file probe fallback
- Probe cache bolt store (or repurpose as passive metadata store)
- `worker/probe.go` — probe worker that queues 187k jobs on startup

### What Stays

- `store.ProbeCache` interface — repurposed as passive metadata store
- Bolt DB backing store — same storage, different population strategy
- `media.ProbeResult` struct — same shape, populated from demux properties instead of avprobe

### Pipeline Flow (new)

```
Pipeline starts → PLAYING → data flows → tvproxydemux properties populated
                                        ↓
                              Go reads properties (video-codec, width, height, etc.)
                                        ↓
                              Writes to passive metadata cache
                                        ↓
                              UI shows metadata on next page load
```

### First Play Experience

First time playing any channel: no metadata in cache. The UI shows the channel name (from M3U/HDHR/SAT>IP). The pipeline starts, plays, and metadata gets cached. The user sees the stream within seconds — no probe delay.

Second time: metadata from last playback is in the cache. UI shows codec badges, resolution, audio info.

### What This Means for Pipeline Building

- `runPipeline()` no longer calls `ensureProbe()` — pipeline starts immediately
- Source codec comes from tvproxydemux properties AFTER data flows (Phase 3 of plugin integration)
- Copy vs transcode decision made by the strategy from the client profile alone
- If the profile says "default" (match source), the pipeline starts in copy mode
- If the demux detects a browser-incompatible codec (MPEG-2), Go rebuilds the pipeline with transcode

### MPEG-2 Copy Mode (agreed approach)

MPEG-2 can't be played in browsers (no MSE support). With the probe removed, we won't know it's MPEG-2 until the pipeline starts.

**Agreed: pipeline rebuild on mismatch, cached for subsequent plays.**

1. First play, no cached metadata: pipeline starts in copy mode. tvproxydemux `stream-detected` signal fires. Go reads `video-codec`. If MPEG-2 and MSE delivery → tear down, rebuild with tvproxydecode + tvproxyencode. One-second penalty, once.
2. Second play onwards: passive cache has codec from last playback. Go builds transcode pipeline immediately. No rebuild.
3. 95% case (h264/h265): copy mode works, no rebuild, zero penalty.

tvproxydemux stays lightweight (demux + parse + audio transcode only). No video transcode in the plugin — that would duplicate hw-accel logic from tvproxydecode/tvproxyencode.

---

## Feature Branch: HTTP Transport + Copy Mode

Branch: `feature/http-transport-copy-mode`

Uncommitted changes:
- Strategy fix: "default" = copy, OutputHeight is a ceiling not a forced target
- MSE copy override removed (the "if codec empty, use h265" hack)
- SAT>IP HTTP transport (RTSP→HTTP:8875 conversion in runPipeline)
- SatIPTransport field on SourceProfile model

Blocked by:
- tvproxyfmp4 keyframe probe fix (plugin agent working on it)
- tvproxyfmp4 stall fix verification
- tvproxysrc HTTP transport property

Once plugin fixes land, merge this branch + enable tvproxyfmp4 + test end-to-end.

---

## Plugin Work (gstreamer-plugin agent)

Full details in `/Users/gavinmcnair/claude/gstreamer-plugin/FIXES-UPDATED.md`

Critical path:
1. tvproxyfmp4 keyframe probe — inspect NALUs, fix sample_flags
2. tvproxyfmp4 H.265 keyframe probe variant
3. tvproxysrc HTTP transport property
4. tvproxyfmp4 stall verification with live streams

After plugin fixes:
5. tvproxydemux video-bitrate property
6. tvproxydemux stream-detected signal (or Go polls after first segment)

---

## Architecture After All Changes

```
Source metadata (M3U tags, Xtream API, channel names)
        ↓
  Passive metadata cache (display only)
        ↓
User clicks play → client profile determines output codec/container
        ↓
Pipeline: tvproxysrc → tvproxydemux → [tvproxydecode] → [tvproxyencode] → tvproxyfmp4
        ↓                    ↓
   MSE segments          Demux properties → update passive cache
        ↓
   Browser playback

No probe. No guessing. Pipeline handles whatever the source delivers.
Client profile decides the output. Strategy resolves copy vs transcode.
```

---

## Code Review Cleanup (from earlier session)

Items 5-11 from the code review still pending:
5. Element nil checks — use mustElement() consistently
6. Merge RunSegmentFlusher into PushVideoFrame (if TrackStore survives)
7. Segment memory compaction
8. AV1 OBU parsing hardening
9. Queue size tuning via source profile
10. Seek improvements (SNAP_BEFORE for VOD)
11. go-gst plugin bin string pipeline issue investigation
