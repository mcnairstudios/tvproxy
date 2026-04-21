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

## Phase 9: Update Protobuf Schema

**Goal**: Sync `proto/tvproxy.proto` with the expanded probe.pb from gstreamer-plugin v1.9.

The plugin repo adds four new fields to the Probe message: `video_bit_depth`, `video_framerate_num`, `video_framerate_den`, `video_bitrate_kbps`. Existing field numbers 1-11 are unchanged — new fields use 12-15. Forward-compatible.

### Changes

1. **`proto/tvproxy.proto`**: Add fields 12-15 to Probe message:
   ```protobuf
   int32 video_bit_depth = 12;          // 8, 10, or 12
   int32 video_framerate_num = 13;      // framerate numerator (e.g., 25)
   int32 video_framerate_den = 14;      // framerate denominator (e.g., 1)
   int32 video_bitrate_kbps = 15;       // estimated bitrate (0 if unknown)
   ```

2. **`pkg/proto/tvproxy.pb.go`**: Regenerate with `protoc --go_out=. proto/tvproxy.proto`

3. **`pkg/session/watcher.go`**: No changes needed — watcher reads whatever fields protobuf provides. New fields are available via `probe.VideoBitDepth`, `probe.VideoFramerateNum`, etc.

4. **`pkg/handler/vod.go:MSEDebug()`**: Include new fields in debug response when probe is available.

### Validation
- `go build ./...` succeeds
- Existing probe.pb tests still pass
- MSEDebug shows bit depth and framerate when available

---

## Phase 10: Wire Decode/Encode Properties

**Goal**: Pass 10-bit decode constraint and explicit element overrides through to pipeline plugins.

**Depends on**: gstreamer-plugin v1.9 (Phases 9-10: tvproxydecode `max-bit-depth` + `element-override`, tvproxyencode `element-override`)

### 10a. SessionOpts new fields

Add to `pkg/gstreamer/spec.go:SessionOpts`:
```go
MaxBitDepth          int    // 0=no limit, 8=force SW decode for 10-bit (A380 BAR)
VideoDecoderElement  string // explicit decoder element from Settings (e.g., "vah264dec")
VideoEncoderElement  string // explicit encoder element from Settings (e.g., "x264enc")
```

### 10b. runPipeline mapping

In `pkg/session/manager.go:runPipeline()`, map from StartOpts to SessionOpts:
```go
MaxBitDepth:         maxBitDepthFromCapability(),
VideoDecoderElement: s.startOpts.VideoDecoderElement,
VideoEncoderElement: s.startOpts.VideoEncoderElement,
```

Where `maxBitDepthFromCapability()` returns 8 if `!gstreamer.Decode10BitSupported()`, else 0.

### 10c. Pipeline builder

In `pkg/session/pipeline_builder.go:addTranscodeChain()`:

**tvproxydecode properties:**
```go
decProps := gstreamer.Props{"hw-accel": decHW}
if opts.MaxBitDepth > 0 {
    decProps["max-bit-depth"] = opts.MaxBitDepth
}
if opts.VideoDecoderElement != "" {
    decProps["element-override"] = opts.VideoDecoderElement
}
spec.AddElement("dec", "tvproxydecode", decProps)
```

**tvproxyencode properties:**
```go
encProps := gstreamer.Props{
    "hw-accel": opts.HWAccel,
    "codec":    opts.OutputCodec,
}
if opts.Bitrate > 0 {
    encProps["bitrate"] = opts.Bitrate
}
if opts.VideoEncoderElement != "" {
    encProps["element-override"] = opts.VideoEncoderElement
}
spec.AddElement("enc", "tvproxyencode", encProps)
```

### Validation
- Settings `decoder_h264=vah264dec` → tvproxydecode gets `element-override=vah264dec`
- `Decode10BitSupported()=false` → tvproxydecode gets `max-bit-depth=8`
- No explicit element set → properties not set, plugin uses defaults
- All existing pipeline builder tests still pass
- Add tests for new property mapping

---

## Phase 11: Wire Source Profile to tvproxysrc

**Goal**: Pass source profile settings through to tvproxysrc properties.

**Depends on**: gstreamer-plugin v1.9 (Phase 11: tvproxysrc `user-agent`, `timeout`, `rtsp-latency`)

### 11a. SessionOpts new fields

Add to `pkg/gstreamer/spec.go:SessionOpts`:
```go
UserAgent     string // HTTP User-Agent override
HTTPTimeout   int    // HTTP timeout in seconds (0=default)
RTSPLatency   int    // RTSP jitterbuffer latency in ms (0=no jitterbuffer)
RTSPTransport string // "tcp" or "udp"
```

### 11b. runPipeline mapping

In `pkg/session/manager.go:runPipeline()`, map from StartOpts:
```go
UserAgent:     s.startOpts.HTTPUserAgent,
HTTPTimeout:   s.startOpts.HTTPTimeoutSec,
RTSPLatency:   s.startOpts.RTSPLatency,
RTSPTransport: s.startOpts.RTSPProtocols,
```

### 11c. Pipeline builder

In `pkg/session/pipeline_builder.go:addSource()`, set properties on tvproxysrc:
```go
func addSource(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
    props := gstreamer.Props{
        "location": opts.SourceURL,
        "is-live":  opts.IsLive,
    }
    if opts.UserAgent != "" {
        props["user-agent"] = opts.UserAgent
    }
    if opts.HTTPTimeout > 0 {
        props["timeout"] = opts.HTTPTimeout
    }
    if opts.RTSPLatency > 0 {
        props["rtsp-latency"] = opts.RTSPLatency
    }
    if opts.RTSPTransport != "" {
        props["rtsp-transport"] = opts.RTSPTransport
    }
    spec.AddElement("src", "tvproxysrc", props)
}
```

### 11d. Update StartOpts comments

Remove the "tvproxysrc handles internally" comments from HTTPUserAgent, HTTPTimeoutSec, RTSPLatency, RTSPProtocols — these are now wired through.

Keep "tvproxysrc handles internally" on HTTPRetries (tvproxysrc has built-in 3-step retry, no property).
Keep "tvproxysrc handles internally" on RTSPBufferMode (no matching tvproxysrc property).

### Validation
- SAT>IP source with RTSPLatency=200 → tvproxysrc gets `rtsp-latency=200`
- IPTV source with custom User-Agent → tvproxysrc gets `user-agent=<value>`
- Source profile with HTTPTimeoutSec=10 → tvproxysrc gets `timeout=10`
- No source profile → properties not set, tvproxysrc uses defaults
- All existing tests still pass

---

## Phase 12: Source Profile Cleanup

**Goal**: Remove obsolete source profile fields from the model and UI.

### 12a. Remove obsolete fields from SourceProfile model

Fields that are confirmed obsolete (executor uses unbounded queues, no plugin property):
- `VideoQueueMs` — remove from model, store, handler, UI
- `AudioQueueMs` — remove from model, store, handler, UI

Fields where plugin handles internally and no property exists:
- `TSSetTimestamps` — tvproxydemux handles timestamps. Remove from model, store, handler, UI.
- `AudioDelayMs` — tvproxydemux PTS fixup handles A/V sync. Remove from model, store, handler, UI.
- `RTSPBufferMode` — no matching tvproxysrc property. Remove from model, store, handler, UI.
- `HTTPRetries` — tvproxysrc has built-in 3-step retry, no property. Remove from model, store, handler, UI.

### 12b. Keep these fields (wired or deferred)

- `AudioLanguage` — wired through to tvproxydemux ✅
- `EncoderBitrateKbps` — wired through to tvproxyencode ✅
- `RTSPProtocols` — wired through to tvproxysrc `rtsp-transport` (Phase 11) ✅
- `RTSPLatency` — wired through to tvproxysrc `rtsp-latency` (Phase 11) ✅
- `HTTPTimeoutSec` — wired through to tvproxysrc `timeout` (Phase 11) ✅
- `HTTPUserAgent` — wired through to tvproxysrc `user-agent` (Phase 11) ✅
- `Deinterlace` + `DeinterlaceMethod` — deferred, needed when native builder adds deinterlace element

### 12c. Update seed defaults

Update the 5 built-in source profiles to remove references to deleted fields. Keep only fields that are wired or deferred.

### 12d. Frontend

Remove the deleted fields from the source profile form in `web/dist/app.js`.

### Validation
- `go build ./...` succeeds
- All tests pass
- Source profile CRUD works with reduced field set
- Frontend form shows only remaining fields
- Existing source profiles in /config/ load without error (JSON ignores unknown fields)

---

## Phase 13: Fix WireGuard Proxy Routing in runPipeline

**Goal**: Ensure WireGuard-flagged streams are routed through the WG localhost proxy in the native pipeline path.

### The Problem

The WireGuard proxy is a persistent localhost HTTP reverse proxy (`127.0.0.1:{port}/?url={original_url}`). It's created once and maintained — useful for both pipeline routing and manual testing/curling. It's deliberately decoupled from the main project.

`runPipeline()` currently uses `m.wgProxyMgr.GetAny()` to find an existing proxy. But `GetAny()` returns nil if no proxy has been created yet. The only code that calls `GetOrCreate()` is the HLS manager's `WGProxyFunc` in `main.go:279`. If the first WG stream goes through the VOD/MSE path (not HLS), the proxy doesn't exist and the stream URL goes direct — bypassing WireGuard.

### Fix

Replace `GetAny()` with `GetOrCreate()` in `runPipeline()`. The manager already has `wgClient` and `config`:

```go
if s.UseWireGuard && m.wgClient != nil && m.wgProxyMgr != nil {
    proxy, err := m.wgProxyMgr.GetOrCreate("default", m.wgClient, m.config, m.log)
    if err == nil {
        pipelineURL = proxy.ProxyURL(s.StreamURL)
        m.log.Info().Str("session_id", s.ID).Str("proxy_url", pipelineURL).Msg("routing through WG proxy")
    } else {
        m.log.Error().Err(err).Str("session_id", s.ID).Msg("failed to create WG proxy")
    }
}
```

`GetOrCreate` is idempotent — returns the existing proxy if already running, creates one if not. The proxy stays running for the lifetime of the process, available for other paths and manual testing.

### Also check

- `UseWireGuard` flows from M3U account → stream model → resolvedStream → StartOpts → Session. Verify each step.
- The proxy only applies to HTTP URLs (`strings.HasPrefix(pipelineURL, "http")`). RTSP SAT>IP streams don't go through WG. Verify this guard is correct.
- RTSP streams: the current code checks `strings.HasPrefix(pipelineURL, "http")` which excludes RTSP. If WG should also cover RTSP sources, the check needs updating. Confirm with current M3U account behaviour — WG is typically IPTV (HTTP) only.

### Validation
- M3U account with UseWireGuard=true → session logs "routing through WG proxy"
- Proxy URL format: `http://127.0.0.1:{port}/?url={encoded_original_url}`
- tvproxysrc receives the proxy URL, not the original stream URL
- Non-WG streams are unaffected (no proxy rewrite)
- WG proxy persists after session ends (available for curl testing)
- Add test: `TestRunPipeline_WireGuardProxyRouting` — verify URL rewrite when UseWireGuard=true

---

## Phase 14: Frontend Updates

**Goal**: Fix recording playback delivery mode and update source profile form to match backend changes.

### 13a. Recording playback missing `delivery` (BUG)

`web/dist/app.js:3793`: The recording playback path builds `recSession` without `delivery`:
```js
var recSession = { id: recResp.session_id, consumer_id: recResp.consumer_id, duration: recResp.duration, container: recResp.container };
```

The backend returns `delivery` (Phase 8), but the frontend doesn't read it. Without `delivery: "mse"`, `openVideoModal` falls back to stream mode — which doesn't work for Browser profile fMP4 output.

**Fix**: Add `delivery: recResp.delivery` to the recSession object.

### 13b. Source profile form — remove obsolete fields

After Phase 12 removes fields from the backend model, the frontend form (`app.js:5939-5972`) must remove:

**Remove from form fields:**
- `video_queue_ms` ("Video Queue (ms)")
- `audio_queue_ms` ("Audio Queue (ms)")
- `ts_set_timestamps` ("Set Timestamps (tsparse)")
- `audio_delay_ms` ("Audio Delay (ms)")
- `rtsp_buffer_mode` ("RTSP Buffer Mode")
- `http_retries` ("HTTP Retries")

**Remove from table columns** (`app.js:5935`):
- `audio_delay_ms` column

**Keep in form:**
- `name`, `deinterlace`, `deinterlace_method` — still needed
- `audio_language` — wired to tvproxydemux
- `rtsp_latency`, `rtsp_protocols` — wired to tvproxysrc (Phase 11)
- `http_timeout_sec`, `http_user_agent` — wired to tvproxysrc (Phase 11)
- `encoder_bitrate_kbps` — wired to tvproxyencode

### 13c. Update source profile table columns

Replace removed columns with more useful ones:
- Keep `name`
- Keep `deinterlace`
- Add `rtsp_latency` (shows ms value, useful for SAT>IP)
- Keep `rtsp_protocols`
- Keep `http_timeout_sec`

### Validation
- Play completed recording from Recordings page → uses MSE, video plays
- Source profile form shows only relevant fields
- Source profile create/update works with reduced fields
- `node web/dist/smoke_test.js` passes

---

## Phase 15: Playback Path Audit — Pre-existing Bugs

**Goal**: Fix issues found during end-to-end playback path review. All are pre-existing (not introduced by Phases 1-14) but block full functionality.

### 15a. VOD Stream handler uses http.ServeFile for growing files

`handler/vod.go:293-314 Stream()` uses `http.ServeFile(w, r, filePath)`. For **live streams** delivered via the session manager's stream pipeline (BuildStreamPipeline → filesink), the file is growing. `http.ServeFile` serves whatever exists and closes — it doesn't tail.

`VODService.TailSession()` exists (line 565) but is never called by any handler.

**Fix**: Replace `http.ServeFile` with `TailSession` for live sessions:
```go
if sess.Duration == 0 {
    reader, err := h.vodService.TailSession(r.Context(), channelID)
    // stream reader to response
} else {
    http.ServeFile(w, r, filePath)
}
```

### 15b. Proxy path missing WireGuard source routing

`ProxyService` doesn't check `stream.UseWireGuard`. When a WG-flagged M3U stream (e.g., regionally restricted IPTV) is played via DLNA/Plex proxy, the upstream HTTP fetch goes direct — bypassing WireGuard. The stream needs WG for the source connection, not the client.

**Fix**: In `proxy.go:openUpstream()`, check `stream.UseWireGuard` and use the WG HTTP client for the upstream fetch.

### 15c. Proxy GStreamer subprocess missing User-Agent

`proxy.go:startGStreamerProxy()` builds `PipelineOpts` without setting `UserAgent`. The `souphttpsrc` element uses the default GStreamer UA. Some IPTV providers block non-browser user agents.

**Fix**: Set `opts.UserAgent = s.config.UserAgent` in `startGStreamerProxy`.

### 15d. Jellyfin/HLS paths missing WireGuard source routing

`jellyfin/playback.go:videoStream()` and `hls/session.go:StartTranscode()` build GStreamer/ffmpeg subprocess pipelines with the raw stream URL. When the M3U source has WireGuard enabled (regional restriction), the subprocess connects directly — bypassing WireGuard.

**Fix**: When stream.UseWireGuard=true, rewrite the stream URL through the WG localhost proxy before passing to the subprocess pipeline (same approach as the native MSE path in runPipeline).

### Validation
- Live stream via VOD Stream handler → data flows continuously (not truncated)
- WG-flagged IPTV stream via DLNA → logs show WG routing
- Proxy GStreamer path logs correct User-Agent
- All existing tests pass

---

## Phase 16: Local File Server for VOD/Recordings

**Goal**: Serve local files (recordings, VOD) over HTTP so tvproxysrc treats them identically to IPTV streams. Eliminates the `IsFileSource` code path — one pipeline topology for everything.

### Why

The native go-gst executor + tvproxysrc `is-live=false` file mode doesn't produce segments through tvproxyfmp4 on macOS (timing/state difference vs `gst_parse_launch`). HTTP sources work perfectly — 1917 (4K HDR, 2 hours) plays through the full pipeline. Serving local files over HTTP makes them behave identically to IPTV VOD: tvproxysrc fetches via HTTP, Range headers enable seeking, one code path for all sources.

### Implementation

New package: `pkg/fileserver/fileserver.go`

```go
package fileserver

type FileServer struct {
    roots []string  // directories to serve (e.g., /record, /config/recordings)
    port  int
    srv   *http.Server
}

func New(roots []string, log zerolog.Logger) *FileServer

func (fs *FileServer) Start() (int, error)  // returns port
func (fs *FileServer) Stop()
func (fs *FileServer) URL(filePath string) string  // converts file path to HTTP URL
```

The handler: `http.ServeFile(w, r, resolvedPath)` — same as tvproxy-streams. Handles Range requests automatically (Go stdlib). Path validation prevents directory traversal.

Route: `GET /file/{hash}` where hash maps to a validated file path. No raw filesystem paths exposed over HTTP.

### Wiring

In `main.go`:
```go
fileSrv := fileserver.New([]string{cfg.RecordDir, cfg.VODOutputDir}, log)
port, _ := fileSrv.Start()
defer fileSrv.Stop()
```

In `StartWatchingFile`: instead of `StreamURL = filePath`, use `StreamURL = fileSrv.URL(filePath)`.

In `BuildRecordingPlaybackPipeline`: remove `IsFileSource` — it's always HTTP now.

### What changes

- `SessionOpts.IsFileSource` becomes unused — all sources are HTTP/RTSP
- No tee bypass for file sources — all sources get the tee + raw recording path (but for recording playback, the raw recording already exists, so the tee writes a duplicate — acceptable, or skip tee when source is localhost)
- Recording playback seeking works via HTTP Range requests to tvproxysrc
- VOD file playback works identically to IPTV VOD

### Validation
- Play a completed recording via the file server → segments produced, MSE plays
- Seek within a recording → tvproxysrc handles Range, pipeline restarts from new position
- `go build ./...` succeeds
- All tests pass

---

## Key Constraints (from INTERFACES.md)

1. go-gst is a dumb executor — no media knowledge in the executor layer
2. Filesystem is the only data interface — no signals, no callbacks
3. Context-driven lifecycle — ctx.Done() triggers SetState(NULL)
4. One pipeline per source — browser and Jellyfin read from the same session directory
5. Protobuf for metadata — probe.pb, signal.pb
6. Plugins decide nothing — tvproxy's profile system decides everything
7. No timestamp hacks — ever
