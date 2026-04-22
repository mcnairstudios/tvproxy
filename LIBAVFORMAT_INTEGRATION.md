# libavformat Integration Plan

## What changed

The input side of tvproxy is moving from GStreamer C plugins (tvproxysrc + tvproxydemux) to Go packages wrapping ffmpeg's libavformat via go-astiav.

**New dependency:** `github.com/mcnairstudios/libavformat`

```bash
go get github.com/mcnairstudios/libavformat
```

## Why

GStreamer's demuxers fail on valid media files that ffmpeg handles:
- qtdemux rejects malformed UDTA boxes (~3% of library)
- matroskademux rejects MKVs with Cues at end of file
- vtdechw crashes on some H264 and decodebin3 doesn't fall back
- GstDiscoverer fails on ~6% of streams

libavformat never fails on valid media. Faster probe (0.15s vs 0.24s). Handles everything VLC handles.

## What changes in tvproxy

### The pipeline changes from:

```
tvproxysrc → tvproxydemux → [tvproxydecode → tvproxyencode] → tvproxyfmp4
```

### To:

```
Go demux loop → appsrc (video) → [parser → decodebin3 → tvproxyencode] → tvproxyfmp4
                appsrc (audio) → [audio decode/transcode chain]         → tvproxyfmp4
```

Go reads packets from libavformat and pushes them into GStreamer via appsrc. The GStreamer output side (decode, encode, mux, segments) is unchanged.

## Packages from mcnairstudios/libavformat

| Package | What it does | Replaces |
|---------|-------------|----------|
| `avprobe` | Probe a URI → StreamInfo (video, audio tracks, subtitles, duration) | GstDiscoverer, tvproxyffprobe |
| `avdemux` | Open URI, read packets in a loop | tvproxysrc + tvproxydemux |
| `avtee` | Tee raw bytes to file while feeding demuxer | tee + queue + filesink for source.ts |
| `demuxloop` | Goroutine: read packets → push to GStreamer appsrc | The glue between libavformat and GStreamer |
| `extradata` | Convert H264/H265 SPS/PPS to GStreamer codec_data caps | New — critical for appsrc caps |
| `pipeline` | Build GStreamer PipelineSpec from probe results | Replaces pipeline_builder.go logic |
| `probepb` | Write probe.pb from probe results | Replaces probe.pb writing in tvproxyfmp4 (early write) |
| `selector` | Pick best audio track (language, skip AD, prefer AAC) | Replaces tvproxydemux audio selection |
| `subtitle` | Extract subtitle text → WebVTT | New |

## Files that change in tvproxy

### Modified

| File | Change |
|------|--------|
| `go.mod` | Add `require github.com/mcnairstudios/libavformat` |
| `pkg/session/manager.go` | Replace `runPipeline()` to use avdemux + appsrc instead of tvproxysrc/tvproxydemux |
| `pkg/session/pipeline_builder.go` | Build appsrc-based pipelines from probe results. Use `pipeline` package from libavformat. |
| `pkg/session/watcher.go` | probe.pb now written by Go (avprobe) immediately. tvproxyfmp4 enriches later with codec_string. |
| `pkg/handler/vod.go` | Use avprobe for stream info instead of waiting for probe.pb. Audio track list available instantly. |
| `cmd/tvproxy/main.go` | Remove GstDiscoverer/tvproxyffprobe init |

### New

| File | Purpose |
|------|---------|
| `pkg/session/demux.go` | Wraps avdemux + demuxloop. Manages the demux goroutine per session. |
| `pkg/session/audio_selector.go` | Uses `selector` package. Picks best audio track from probe, handles runtime switch. |

### No longer needed (but don't delete yet — keep until validated)

| File | Reason |
|------|--------|
| `pkg/gstreamer/prober.go` | avprobe replaces it |
| `pkg/avprobe/` (old C-based) | Go avprobe replaces it |
| `pkg/worker/probe.go` | No probe queue needed — probe is instant |

## How the session manager changes

### Before (current runPipeline):

```go
func (m *Manager) runPipeline(s *Session) {
    spec := BuildMSEPipeline(SessionOpts{
        SourceURL: s.StreamURL,
        IsLive:    s.IsLive,
        // ...
    }, s.OutputDir)
    m.executor.Run(ctx, spec)
}
```

### After:

```go
func (m *Manager) runPipeline(s *Session) {
    // 1. Probe (instant, never fails)
    info, _ := avprobe.Probe(s.StreamURL, 5)

    // 2. Write probe.pb immediately (frontend can render)
    probepb.Write(filepath.Join(s.OutputDir, "probe.pb"), info)

    // 3. Select audio track
    audioIdx := selector.BestAudio(info.AudioTracks, s.AudioLanguage)

    // 4. Open demuxer
    demuxer, _ := avdemux.NewDemuxer(s.StreamURL, avdemux.Opts{
        AudioTrack: audioIdx,
        Timeout:    10,
    })
    defer demuxer.Close()

    // 5. Build GStreamer pipeline (appsrc-based, from probe results)
    spec := pipeline.Build(info, pipeline.Opts{
        OutputDir:  s.OutputDir,
        HWAccel:    s.HWAccel,
        VideoCodec: s.OutputVideoCodec,
        Bitrate:    s.Bitrate,
        IsLive:     s.IsLive,
    })

    // 6. Start GStreamer pipeline
    pipe, appsrcs := m.executor.RunWithAppsrc(ctx, spec)

    // 7. Start demux loop (goroutine reads packets, pushes to appsrc)
    demuxloop.Run(ctx, demuxer, appsrcs.Video, appsrcs.Audio)
}
```

### What the executor needs

A new method: `RunWithAppsrc()` that returns handles to the appsrc elements so the demux loop can push buffers:

```go
type AppsrcHandles struct {
    Video *gst.Element  // appsrc for video
    Audio *gst.Element  // appsrc for audio (nil for video-only)
}

func (e *Executor) RunWithAppsrc(ctx context.Context, spec PipelineSpec) (*gst.Pipeline, *AppsrcHandles, error)
```

The executor is still dumb — it creates elements, links them, starts the pipeline. It just also returns references to the appsrc elements.

## GStreamer pipeline templates (from libavformat/pipeline package)

### MSE transcode (most common):
```
appsrc name=videosrc caps="video/x-h264,..." ! h264parse ! decodebin3 ! tvproxyencode ! tvproxyfmp4.video
appsrc name=audiosrc caps="audio/x-ac3,..." ! decodebin3 ! audioconvert ! audioresample ! faac ! aacparse ! tvproxyfmp4.audio
```

### MSE AAC passthrough:
```
appsrc name=videosrc caps="video/x-h264,..." ! h264parse ! decodebin3 ! tvproxyencode ! tvproxyfmp4.video
appsrc name=audiosrc caps="audio/mpeg,mpegversion=4,..." ! aacparse ! tvproxyfmp4.audio
```

### Stream copy (Jellyfin/DLNA):
```
appsrc name=videosrc caps="video/x-h264,..." ! h264parse ! tvproxymux.video
appsrc name=audiosrc caps="audio/x-ac3,..." ! ac3parse ! tvproxymux.audio
tvproxymux output-format=mpegts ! fdsink
```

### Video-only:
```
appsrc name=videosrc caps="video/x-h265,..." ! h265parse ! decodebin3 ! tvproxyencode ! tvproxyfmp4.video
```
No audio appsrc. No silence hack. tvproxyfmp4 handles video-only natively.

## Raw source recording

The `avtee` package handles tee'ing raw bytes to disk. It wraps the HTTP response body before feeding it to libavformat's custom I/O:

```go
tee := avtee.New(sourceFile)
demuxer, _ := avdemux.NewDemuxerWithReader(tee, avdemux.Opts{...})
// Raw bytes go to sourceFile, demuxed packets go to appsrc
```

Single HTTP connection. No GStreamer tee element. No queue deadlock risk.

## Audio track switching at runtime

```go
// Frontend: user picks track 3 (French)
s.demuxer.SetAudioTrack(3)
// Demuxer now reads packets from the new audio stream
// If codec changed (e.g. AC3 → AAC): flush audio appsrc, reconfigure caps
// If same codec: seamless switch
```

No input-selector element. No GStreamer involvement. Go just reads from a different stream index.

## Seeking (VOD)

```go
// Browser sends seek to 5 minutes
s.demuxer.Seek(5 * 60 * 1000)  // 300000ms
// Flush appsrcs
appsrcs.Video.EndOfStream()
appsrcs.Audio.EndOfStream()
// Restart pipeline (or flush and continue — depends on tvproxyfmp4 seek support)
```

## PTS handling in the demux loop

Three fixups applied in Go before pushing to appsrc:

```go
// 1. Base PTS normalisation (replaces streamsynchronizer)
if basePTS < 0 { basePTS = pkt.PTS }
pkt.PTS -= basePTS
pkt.DTS -= basePTS

// 2. Missing DTS (MKV sources)
if pkt.DTS == NoPTS { pkt.DTS = pkt.PTS }

// 3. Sample-accurate audio PTS (Chrome drift fix)
if pkt.Type == Audio {
    pkt.PTS = audioBasePTS + frameCount * 1024 * GST_SECOND / sampleRate
    pkt.DTS = pkt.PTS
    frameCount++
}
```

## Error handling

No more GStreamer handle_message, error latching, or pad probe gates. Errors are Go errors:

```go
pkt, err := demuxer.ReadPacket()
if err == io.EOF {
    appsrcs.Video.EndOfStream()
    appsrcs.Audio.EndOfStream()
    return // VOD complete
}
if err != nil {
    log.Error().Err(err).Msg("demux error")
    // retry or signal session failure
}
```

## Retry logic

**Demux-level** (replaces tvproxysrc 3-step backoff):
Built into avdemux — reconnects on transient HTTP errors with 1s, 2s, 4s backoff.

**Session-level** (unchanged):
Session manager creates new pipeline on fatal failure.

## Chase-play / growing file

libavformat supports `follow=1` on the file protocol natively:

```go
demuxer, _ := avdemux.NewDemuxer("/record/ch1/uuid/source.ts", avdemux.Opts{
    Follow: true,
    FormatHint: "mpegts",
})
```

Retries on EOF automatically. No custom tail logic needed.

## What stays in GStreamer C plugins

| Plugin | Stays | Why |
|--------|-------|-----|
| tvproxyencode | YES | HW encoder selection, gldownload, autodeinterlace, videoconvertscale |
| tvproxyfmp4 | YES | cmafmux, keyframe probe, ISO BMFF parsing, codec_string, segment output |
| tvproxymux | YES | MP4/MPEG-TS muxing for stream copy |
| tvproxydecode | YES | HW decoder selection (used via decodebin3) |

## What gets removed from GStreamer C plugins

| Plugin | Why |
|--------|-----|
| tvproxysrc | libavformat handles source |
| tvproxydemux | libavformat handles demux |

Don't delete the code yet. Keep it in the repo but remove from `plugin.c` registration once libavformat path is validated.

## Migration order

1. **Add `go get github.com/mcnairstudios/libavformat`** to tvproxy
2. **Build `pkg/session/demux.go`** — wraps avdemux + demuxloop per session
3. **Update `pipeline_builder.go`** — generate appsrc-based specs from probe results
4. **Update `executor.go`** — add `RunWithAppsrc()` returning appsrc handles
5. **Update `manager.go:runPipeline()`** — use probe → demux → appsrc flow
6. **Test against 100 streams** — every stream ffprobe reads should produce segments
7. **Remove tvproxysrc/tvproxydemux from plugin.c** — once validated
8. **Delete old probe/discover code** — once validated

## Backwards compatibility

The PipelineSpec structure is unchanged. The executor is unchanged (just gains one method). The filesystem interface is unchanged. probe.pb schema is unchanged (just written earlier by Go). The session manager API is unchanged.

The change is internal to how `runPipeline()` works. Nothing outside the session package needs to know.
