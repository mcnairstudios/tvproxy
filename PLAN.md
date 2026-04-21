# tvproxy Plan

Reference: `/Users/gavinmcnair/claude/gstreamer-plugin/INTERFACES.md` for the full contract.

## Current State

tvproxy builds GStreamer pipelines using go-gst. Two code paths:
- **TrackStore path** (active): Go builds fMP4 from raw samples via appsink. 659 lines. Works.
- **Plugin path** (disabled): tvproxyfmp4 produces segments via signals. Broken — go-gst string pipeline ghost pad issue.

Both paths are being replaced. The new architecture uses the filesystem as the interface between GStreamer and Go. No signals, no appsink, no TrackStore.

---

## Architecture

```
┌─────────────────────────────────────────────┐
│  Profile Layer                               │
│  Client profile → PipelineSpec               │
│  Knows: codecs, hw-accel, bitrate            │
├─────────────────────────────────────────────┤
│  Executor Layer                              │
│  PipelineSpec → running pipeline             │
│  Knows: go-gst API only                      │
├─────────────────────────────────────────────┤
│  Filesystem                                  │
│  /record/<streamid>/<uuid>/                  │
│  GStreamer writes, Go reads and serves       │
├─────────────────────────────────────────────┤
│  GStreamer C plugins                         │
│  Writes: source.ts, segments/, probe.pb      │
└─────────────────────────────────────────────┘
```

### Key decisions (from INTERFACES.md)

- go-gst: native element creation only, no string pipelines, dumb executor
- Filesystem is the only data interface — no signals, no callbacks
- One pipeline per source — browser reads segments, Jellyfin reads source.ts
- Recording: dual write (raw source + segments). Segments deleted on shutdown. Raw source is archival copy.
- Protobuf for metadata files (probe.pb, signal.pb)
- Plugins execute, tvproxy decides everything (codec, bitrate, hw-accel, delivery mode)

---

## Phase 1: Three-Layer Refactor

**Goal**: Separate pipeline building into Profile Layer + Executor Layer.

### 1a. PipelineSpec types

New file: `pkg/gstreamer/spec.go`

```go
type PipelineSpec struct {
    Elements []ElementSpec
    Links    []LinkSpec
}

type ElementSpec struct {
    Name       string
    Factory    string
    Properties map[string]any
}

type LinkSpec struct {
    FromElement string
    FromPad     string
    ToElement   string
    ToPad       string
}
```

### 1b. Executor

New file: `pkg/gstreamer/executor.go`

The executor takes a PipelineSpec + context + output directory, and:
1. Creates a GStreamer pipeline
2. Creates each element by factory name (`gst.NewElement`)
3. Sets properties by key/value
4. Adds all elements to the pipeline
5. Links pads as specified
6. Starts a bus watch for EOS and ERROR
7. Sets state to PLAYING
8. On ctx.Done() → SetState(NULL)

The executor has zero media knowledge. It never imports codec types, never checks property values, never knows what a demuxer is.

```go
func (e *Executor) Run(ctx context.Context, spec PipelineSpec) error {
    pipe, _ := gst.NewPipeline("tvproxy")
    elements := map[string]*gst.Element{}

    for _, el := range spec.Elements {
        elem, _ := gst.NewElement(el.Factory)
        for k, v := range el.Properties {
            elem.SetProperty(k, v)
        }
        pipe.Add(elem)
        elements[el.Name] = elem
    }

    for _, link := range spec.Links {
        from := elements[link.FromElement]
        to := elements[link.ToElement]
        if link.FromPad != "" && link.ToPad != "" {
            // Try static pad first, fall back to request pad.
            // tvproxydemux has static pads (video, audio).
            // tvproxyfmp4 has request pads (video, audio).
            fromPad := from.GetStaticPad(link.FromPad)
            if fromPad == nil {
                fromPad = from.GetRequestPad(link.FromPad)
            }
            toPad := to.GetStaticPad(link.ToPad)
            if toPad == nil {
                toPad = to.GetRequestPad(link.ToPad)
            }
            fromPad.Link(toPad)
        } else {
            from.Link(to)
        }
    }

    pipe.SetState(gst.StatePlaying)

    // Bus watch in goroutine
    // ctx.Done() → pipe.SetState(gst.StateNull)
}
```

### 1c. Profile layer builds PipelineSpec

New file: `pkg/session/pipeline_builder.go`

Builds PipelineSpec from session options. All media knowledge lives here:

```go
func BuildMSEPipeline(opts SessionOpts, outputDir string) PipelineSpec {
    spec := PipelineSpec{}
    spec.AddElement("src", "tvproxysrc", Props{
        "location": opts.SourceURL,
        "is-live":  opts.IsLive,
    })
    // tee for raw source recording (network sources only)
    if !opts.IsFileSource {
        spec.AddElement("tee", "tee", nil)
        // Queues on BOTH tee branches — prevents backpressure deadlock
        spec.AddElement("q_demux", "queue", Props{
            "max-size-buffers": 0, "max-size-time": 0, "max-size-bytes": 0,
        })
        spec.AddElement("q_raw", "queue", Props{
            "max-size-buffers": 0, "max-size-time": 0, "max-size-bytes": 0,
        })
        spec.AddElement("rawsink", "filesink", Props{
            "location": filepath.Join(outputDir, "source.ts"),
            "async":    false,  // CRITICAL: prevents preroll deadlock with tee + tvproxydemux
        })
        spec.Link("src", "tee")
        spec.LinkPads("tee", "src_0", "q_demux", "sink")
        spec.Link("q_demux", "d")
        spec.LinkPads("tee", "src_1", "q_raw", "sink")
        spec.Link("q_raw", "rawsink")
    } else {
        spec.Link("src", "d")
    }
    spec.AddElement("d", "tvproxydemux", Props{
        "audio-channels": 2,
    })
    if opts.NeedsTranscode {
        spec.AddElement("dec", "tvproxydecode", Props{"hw-accel": opts.HWAccel})
        spec.AddElement("enc", "tvproxyencode", Props{
            "hw-accel": opts.HWAccel,
            "codec":    opts.VideoCodec,
            "bitrate":  opts.Bitrate,
        })
        spec.LinkPads("d", "video", "dec", "sink")
        spec.Link("dec", "enc")
        spec.LinkPads("enc", "src", "fmp4", "video")
    } else {
        spec.LinkPads("d", "video", "fmp4", "video")
    }
    spec.LinkPads("d", "audio", "fmp4", "audio")
    spec.AddElement("fmp4", "tvproxyfmp4", Props{
        "output-dir":          filepath.Join(outputDir, ""),
        "video-codec":         opts.VideoCodec,
        "segment-duration-ms": 2000,
    })
    return spec
}
```

Similar functions for `BuildStreamPipeline()`, `BuildStreamTranscodePipeline()`.

### Validation
- Build a PipelineSpec from a test profile, verify it contains correct elements
- Run executor with a local .ts file, verify pipeline reaches PLAYING and files appear

---

## Phase 2: Protobuf Integration + Filesystem Watcher

**Goal**: Read probe.pb using Go protobuf, watch the session directory for new files, serve them.

### 2a. Import shared .proto (moved from Phase 6 — needed here)

Copy `proto/tvproxy.proto` from gstreamer-plugin repo. Generate Go code:

```bash
protoc --go_out=. proto/tvproxy.proto
```

### 2b. Directory watcher

New file: `pkg/session/watcher.go`

Uses `fsnotify` to watch the session directory:
- `probe.pb` appears → deserialize with protobuf, cache stream metadata
- `segments/init_video.mp4` appears → video init ready
- `segments/video_NNNN.m4s` appears → new video segment
- Same for audio

### 2c. HTTP serving

Serve segment files directly from disk. Minimal Go code:

```go
// GET /mse/<sessionID>/segments/init_video.mp4
// GET /mse/<sessionID>/segments/video_0001.m4s
// GET /mse/<sessionID>/probe.pb → JSON response with codec string etc.
```

For the browser:
- Fetch probe.pb (as JSON via Go endpoint) → get codec string → create SourceBuffers
- Fetch init segments → append to SourceBuffers
- Poll for new .m4s files by incrementing seq number → append
- Detect seek/restart: Go serves a session generation counter (incremented on pipeline restart). Browser polls it. On change → reset SourceBuffers, re-fetch init segments, restart from seq 1.

For Jellyfin:
- Serve source.ts via HTTP with chunked transfer encoding (growing file)

### 2d. Session lifecycle

```go
type Session struct {
    ID        string
    StreamID  string
    OutputDir string    // /record/<streamid>/<uuid>/
    Recorded  bool      // true = keep on close, false = delete
    Cancel    context.CancelFunc
}
```

On session close:
1. Cancel context → pipeline stops → GStreamer deletes segments/
2. If `!Recorded` → Go deletes the entire session directory
3. If `Recorded` → source.ts + probe.pb remain

### Validation
- Start pipeline, watch for probe.pb, verify fields
- Watch for segment files, serve via HTTP, verify browser can fetch
- Close session without recording → directory deleted
- Close session with recording → source.ts + probe.pb remain

---

## Phase 3: Delete Old Code

**Goal**: Remove everything replaced by the filesystem interface.

### Delete list

| File | Reason |
|------|--------|
| `pkg/fmp4/trackstore.go` (659 lines) | Replaced by tvproxyfmp4 filesystem output |
| `pkg/fmp4/av1.go` | AV1 init segment building — plugin handles it |
| `pkg/gstreamer/prober.go` | Probe scheduler — replaced by probe.pb |
| `pkg/avprobe/avprobe.go` | C libavformat probe — eliminated |
| `pkg/avprobe/probe_reader.go` | ffprobe subprocess — eliminated |
| `pkg/worker/probe.go` | Probe worker queue — eliminated |
| Signal connection code in manager.go | No signals — filesystem interface |
| `useFmp4Plugin` flag | No flag — filesystem is the only path |
| String pipeline building in pipeline.go | Native element creation only |
| Old native pipeline building in builder.go | Replaced by PipelineSpec |

### Keep

| File | Reason |
|------|--------|
| `pkg/fmp4/store.go` | Store interface — repurposed for filesystem-backed store |
| Session manager core | Lifecycle, consumer tracking, seeking |
| Client profile system | Unchanged — drives PipelineSpec building |
| Bolt DB cache | Repurposed as passive metadata cache from probe.pb |

### Validation
- `go build ./...` succeeds
- `go vet ./...` clean
- All existing tests pass (update tests that depend on deleted code)
- Play SAT>IP, HDHR, IPTV live, IPTV VOD — all working

---

## Phase 4: Recording

**Goal**: Dual write (source.ts + segments) with record/delete lifecycle.

### 4a. Recording toggle

Session gets a `Record()` method that sets `Recorded = true`. UI button calls this.

### 4b. On session close

- `Recorded = false`: delete entire `/record/<streamid>/<uuid>/`
- `Recorded = true`: GStreamer already deleted segments/. source.ts + probe.pb remain. These are the permanent recording.

### 4c. Recording playback

Playing back a recording is just another pipeline:
```go
spec := BuildMSEPipeline(SessionOpts{
    SourceURL:    filepath.Join(recordDir, "source.ts"),
    IsLive:       false,
    IsFileSource: true,  // no tee, no source.ts write
}, newOutputDir)
```

Same plugins, same directory layout, same browser code. The source is a local file.

### Validation
- Record a stream, close session → source.ts + probe.pb persist
- Play back recording → segments appear, browser plays
- Don't record, close session → directory gone

---

## Phase 5: Passthrough for Stream Clients

**Goal**: Jellyfin/Plex/DLNA get full quality passthrough.

### 5a. Stream pipeline builder

```go
func BuildStreamPipeline(opts SessionOpts, outputDir string) PipelineSpec {
    // tvproxysrc → tee → queue → tvproxydemux(audio-codec=copy, audio-channels=0)
    //                   → queue → filesink(source.ts, async=false)
    //           → tvproxymux → fdsink
    // NOTE: queues on both tee branches, async=false on filesink
}
```

Profile sets `audio-codec=copy audio-channels=0` for passthrough. 4K HEVC + surround sound straight through.

### 5b. Stream transcode pipeline builder

For clients that need transcoding (e.g., DLNA device needing H.264):

```go
func BuildStreamTranscodePipeline(opts SessionOpts, outputDir string) PipelineSpec {
    // tvproxysrc → tee → queue → tvproxydemux → tvproxydecode → tvproxyencode → tvproxymux → fdsink
    //                   → queue → filesink(source.ts, async=false)
    // NOTE: queues on both tee branches, async=false on filesink
}
```

### Validation
- Jellyfin connects → gets MPEG-TS stream
- Source is HEVC + AC3 → Jellyfin receives HEVC + AC3 (passthrough)
- DLNA device needs H.264 → gets transcoded H.264 + AAC

---

## Phase 6: CI Tests

### Go unit tests

- PipelineSpec builder: profile → correct elements and links for each pipeline template
- Filesystem watcher: mock directory, verify event handling
- Session lifecycle: create, record, close, verify cleanup
- probe.pb deserialization: verify all fields

### Integration tests (require Docker image)

- Start pipeline with test fixture .ts file in Docker
- Verify: segments appear, probe.pb written, cleanup on shutdown
- Run in CI with `gavinmcnair/gstreamer:1.8.1` image

---

## Files That Change

### New files

| File | Purpose |
|------|---------|
| `pkg/gstreamer/spec.go` | PipelineSpec types |
| `pkg/gstreamer/executor.go` | Dumb pipeline executor |
| `pkg/session/pipeline_builder.go` | Profile → PipelineSpec (MSE, stream, transcode variants) |
| `pkg/session/watcher.go` | Filesystem watcher for session directory |
| `proto/tvproxy.proto` | Shared protobuf schema (copy from gstreamer-plugin) |
| `pkg/proto/tvproxy.pb.go` | Generated protobuf Go code |

### Modified files

| File | Change |
|------|--------|
| `pkg/session/manager.go` | Use executor + watcher instead of direct go-gst, remove signal handlers, remove useFmp4Plugin flag |
| `pkg/session/session.go` | Add OutputDir, Recorded fields, Record() method |
| `pkg/handler/mse.go` | Serve segment files from disk instead of from SegmentStore |
| `cmd/tvproxy/main.go` | Remove probe scheduler, remove avprobe init |

### Deleted files

| File | Lines | Reason |
|------|-------|--------|
| `pkg/fmp4/trackstore.go` | 659 | Replaced by filesystem |
| `pkg/fmp4/av1.go` | ~200 | Plugin handles AV1 |
| `pkg/gstreamer/prober.go` | ~300 | Replaced by probe.pb |
| `pkg/avprobe/avprobe.go` | ~200 | Eliminated |
| `pkg/avprobe/probe_reader.go` | ~150 | Eliminated |
| `pkg/worker/probe.go` | ~100 | Eliminated |
| `pkg/gstreamer/pipeline.go` | ~180 | String pipelines eliminated |

Estimated: ~1800 lines deleted, ~500 lines added.

---

## Phase 7: Fix needsTranscode + Source Codec Tracking (CRITICAL)

**Goal**: Fix the bug where transcode pipelines are never selected.

### The Bug

`runPipeline()` in manager.go:578-580:
```go
outCodec := gstreamer.NormalizeCodec(s.startOpts.OutputVideoCodec)
srcCodec := gstreamer.NormalizeCodec(s.OutputVideoCodec)
needsTranscode := outCodec != "" && outCodec != "copy" && outCodec != "default" && outCodec != srcCodec
```

Both `s.startOpts.OutputVideoCodec` and `s.OutputVideoCodec` are set from the same value (`opts.OutputVideoCodec`). They are always equal. `needsTranscode` is always false.

Compare with the working string pipeline builder in `pipeline.go:124-125`:
```go
outCodec := NormalizeCodec(opts.OutputVideoCodec)  // target codec
srcCodec := NormalizeCodec(opts.VideoCodec)         // source codec from probe
```

### Fix

1. Add `SourceVideoCodec` field to `StartOpts` in manager.go
2. In `vod.go:StartWatching()`: set `startOpts.SourceVideoCodec = probeVCodec` (already resolved at line 307)
3. In `vod.go:StartWatchingStream()`: set `startOpts.SourceVideoCodec = stream.VODVCodec` (already available)
4. In `vod.go:StartWatchingFile()`: set from probe cache if available
5. In `runPipeline()`: use `s.startOpts.SourceVideoCodec` for the source codec:

```go
outCodec := gstreamer.NormalizeCodec(s.startOpts.OutputVideoCodec)
srcCodec := gstreamer.NormalizeCodec(s.startOpts.SourceVideoCodec)
needsTranscode := outCodec != "" && outCodec != "copy" && outCodec != "default" && outCodec != srcCodec
```

6. Map `srcCodec` (not `outCodec`) to `SessionOpts.VideoCodec` — this is the source codec field that pipeline_builder.go uses for `resolveOutputCodec()`.

### Validation
- MPEG-2 SAT>IP → Browser (H.264 profile): `needsTranscode=true`, decoder+encoder chain in spec
- H.264 IPTV → Browser (copy profile): `needsTranscode=false`, no decoder/encoder
- H.265 IPTV → Jellyfin (copy): `needsTranscode=false`, stream passthrough
- Unknown source (no probe cache): `srcCodec=""`, `needsTranscode=true` if outCodec set (safe default)

---

## Phase 8: Handler Fixes + Source Profile Documentation

**Goal**: Fix HTTP response issues that break frontend playback, and document source profile field status.

### 9a. PlayCompletedRecording missing `delivery` response field

`handler/vod.go:PlayCompletedRecording()` returns session_id, consumer_id, container, duration, audio_only — but NOT `delivery`. The frontend needs this to decide MSE vs stream mode.

Fix: Add `delivery` to the response, same pattern as `CreateChannelSession()`:
```go
delivery := "stream"
if sess := h.vodService.GetSession(sessionID); sess != nil && sess.Delivery == "mse" {
    delivery = "mse"
}
resp["delivery"] = delivery
```

### 9b. MSEInit returns 410 for "init not ready"

`handler/vod.go:594-597`: When init segments haven't been written yet, the handler returns `410 Gone`. The MSE worker interprets 410 as "generation changed — restart", creating a restart loop during pipeline startup.

Fix: Return `503 Service Unavailable` instead. The worker already handles non-200 responses with backoff retry.

### 9c. Invalid track name validation

`MSEInit` and `MSESegment` don't validate the `track` URL parameter. Invalid tracks (not "video" or "audio") fall through to nil data checks and return misleading status codes.

Fix: Add early return with `400 Bad Request` for unrecognized track names.

### 9d. Comment unmapped source profile settings in runPipeline

Many source profile settings in `StartOpts` are populated by `applySourceProfile()` in vod.go but are NOT mapped to `SessionOpts` in `runPipeline()`. This is intentional — the GStreamer plugins now handle these internally:

- `HTTPTimeoutSec`, `HTTPRetries`, `HTTPUserAgent` → tvproxysrc handles HTTP internally
- `RTSPProtocols`, `RTSPBufferMode` → tvproxysrc handles RTSP internally
- `RTSPLatency` → tvproxysrc uses GStreamer default (higher than ideal for SAT>IP, but acceptable)
- `TSSetTimestamps` → tvproxydemux handles timestamps
- `VideoQueueMs`, `AudioQueueMs` → executor uses unbounded queues
- `AudioDelayMs` → tvproxydemux handles A/V sync
- `Deinterlace`, `DeinterlaceMethod` → deferred; no element in native builder yet
- `OutputHeight` → deferred; no videoscale element in native builder yet

Add a comment block in `runPipeline()` documenting which StartOpts fields are intentionally unmapped and why, so they are not accidentally removed during cleanup. These settings remain in the StartOpts struct and source profile UI for future use.

Also add a comment in `StartOpts` struct noting which fields are for the string pipeline builder (proxy.go) vs the native pipeline builder, and which are deferred.

### Validation
- PlayCompletedRecording response includes `delivery: "mse"` when Browser profile used
- MSEInit returns 503 (not 410) when init segments not yet written
- MSEInit with `track=badvalue` returns 400
- `go build ./...` succeeds
- All tests pass

---

## Next Steps (Require GStreamer Plugin Changes)

These require new properties on the C plugins before tvproxy can wire them through. Not blockers for initial playback — the plugins handle reasonable defaults — but needed for full parity with the string pipeline builder.

### 10-Bit Decode Control

**Problem**: A380 GPU reports 10-bit decode support but fails at runtime due to 256MB BAR limit (Above 4G Decoding not enabled in BIOS). The string pipeline builder handles this in Go via `resolveDecodeHW()` which forces SW decode when source is 10-bit and `Decode10BitSupported()=false`.

The native path can't do this reliably because source bit depth isn't always known at pipeline build time (probe cache may miss). tvproxydecode selects the decoder at negotiation time when it DOES know the bit depth.

**Solution**: Add `max-bit-depth` property to tvproxydecode in the plugin repo. When set to 8, the plugin forces SW decode for anything above 8-bit. tvproxy sets it based on `Decode10BitSupported()`.

**Plugin change**: tvproxydecode gains `max-bit-depth` (int, default 0 = no limit)
**tvproxy change**: `SessionOpts.MaxBitDepth`, pipeline builder sets property on tvproxydecode

### Explicit Encoder/Decoder Element Overrides

**Problem**: Settings UI allows per-codec element selection (`encoder_h264`, `decoder_h265`, etc.). The string pipeline builder (pipeline.go:151-153, 228-229) bypasses tvproxydecode/tvproxyencode when explicit elements are set. The native pipeline builder currently ignores these overrides — it always uses tvproxydecode/tvproxyencode.

**Solution**: Add `element-override` property to both tvproxydecode and tvproxyencode. When set, the plugin uses the specified element instead of its internal selection logic.

**Plugin changes**:
- tvproxydecode gains `element-override` (string, default "" = auto-select)
- tvproxyencode gains `element-override` (string, default "" = auto-select)

**tvproxy change**: Map `VideoDecoderElement`/`VideoEncoderElement` from StartOpts through to element properties.

Alternative (no plugin change): Skip tvproxydecode/tvproxyencode entirely when explicit elements are set, using the raw element + parser directly in the pipeline builder. This matches the string builder's approach but adds complexity to pipeline_builder.go.

### Source Profile Simplification

Most source profile settings are now handled by GStreamer plugins internally. A cleanup pass should:

1. Audit which source profile fields tvproxysrc/tvproxydemux actually expose as properties
2. Wire through any that are supported but not yet mapped (e.g., `rtsp-transport` on tvproxysrc)
3. Remove or deprecate settings that plugins handle automatically
4. Consider exposing RTSPLatency — GStreamer default (2000ms) is too high for SAT>IP live

Settings to evaluate:
- `HTTPTimeoutSec`, `HTTPRetries`, `HTTPUserAgent` → tvproxysrc may expose these
- `RTSPLatency`, `RTSPProtocols`, `RTSPBufferMode` → tvproxysrc has `rtsp-transport` but not latency
- `Deinterlace`, `DeinterlaceMethod` → no plugin support, would need separate element in native builder
- `OutputHeight` → no plugin support, would need videoscale element in native builder
- `AudioDelayMs` → tvproxydemux may handle this

---

## Key Constraints (from INTERFACES.md)

1. go-gst is a dumb executor — no media knowledge in the executor layer
2. Filesystem is the only data interface — no signals, no callbacks
3. Context-driven lifecycle — ctx.Done() triggers SetState(NULL)
4. One pipeline per source — browser and Jellyfin read from the same session directory
5. Protobuf for metadata — probe.pb, signal.pb
6. Plugins decide nothing — tvproxy's profile system decides everything
7. No timestamp hacks — ever
