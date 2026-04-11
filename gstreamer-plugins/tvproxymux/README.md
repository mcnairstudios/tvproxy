# tvproxymux

A GStreamer plugin that provides a single bin element (`tvproxymux`) for muxing live video and audio into fragmented MP4 or MPEG-TS containers. It auto-detects the video codec, inserts the correct parser, and applies proven muxer settings so you don't have to.

Written in Go using [go-gst](https://github.com/go-gst/go-gst).

## Why this exists

Every GStreamer pipeline that outputs video requires a specific combination of parser + muxer + settings that depends on the input codec and the target container. Getting any part wrong produces broken output:

| Mistake | Result |
|---------|--------|
| Missing `config-interval=-1` on h264parse/h265parse | Players can't decode (no SPS/PPS parameter sets on keyframes) |
| Using `isofmp4mux` instead of `mp4mux` | 12-second delay to first byte |
| Missing `streamable=true` on mp4mux | File not playable while being written |
| Wrong parser for codec | Pipeline fails silently or produces garbage |
| Setting `latency=0` on mpegtsmux | Breaks video |
| Setting `alignment=-1` on mpegtsmux | Breaks video |

These were all discovered through exhaustive testing against HDHomeRun and SAT>IP DVB-T/T2 streams. This plugin bakes the correct formula into a single element so pipelines can't get it wrong.

### What it replaces

```
# Without the plugin (error-prone, must be correct for each codec):
... ! h264parse config-interval=-1 ! mp4mux fragment-duration=500 streamable=true ! filesink ...
    aacparse ! mp4mux.

# With the plugin:
... ! tvproxymux name=m ! filesink ...
    ! m.
```

## Element overview

`tvproxymux` is a `GstBin` subclass with:

- **`video`** request sink pad -- accepts `video/x-h264`, `video/x-h265`, `video/mpeg` (mpeg2)
- **`audio`** request sink pad -- accepts `audio/mpeg` (AAC), `audio/x-ac3`, `audio/x-eac3`
- **`src`** always-present source pad -- outputs `video/quicktime` (MP4) or `video/mpegts`

### Properties

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `output-format` | string | `"mp4"` | `"mp4"` for browser-compatible fragmented MP4, `"mpegts"` for MPEG-TS |
| `fragment-duration` | uint | `500` | Fragment duration in milliseconds for MP4 mode (range: 100-1000, ignored for mpegts) |

### Internal pipeline

When pads are requested and linked, the bin constructs this internal chain:

**MP4 mode** (default):
```
video pad --> {auto-detected parser} config-interval=-1 --> mp4mux fragment-duration=500 streamable=true --> src pad
audio pad --> aacparse ------------------------------------/
```

**MPEG-TS mode** (`output-format=mpegts`):
```
video pad --> {auto-detected parser} config-interval=-1 --> mpegtsmux (default settings) --> src pad
audio pad --> aacparse ------------------------------------/
```

### Codec auto-detection

The video codec is detected from input caps. The correct parser is inserted automatically:

| Input caps | Parser | config-interval |
|-----------|--------|-----------------|
| `video/x-h264` | `h264parse` | `-1` |
| `video/x-h265` | `h265parse` | `-1` |
| `video/mpeg` (mpegversion=2) | `mpegvideoparse` | n/a |

Detection happens either at pad request time (if caps are provided) or via a pad probe when the first caps event arrives. No manual parser selection is needed.

## Prerequisites

- Go 1.21+
- GStreamer 1.20+ development libraries
- `gst-plugin-gen` tool (built from go-gst, see below)

### macOS (Homebrew)

```bash
brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad gst-plugins-ugly gst-libav
```

### Install gst-plugin-gen

```bash
go install github.com/go-gst/go-gst/cmd/gst-plugin-gen@latest
```

Ensure `$GOPATH/bin` (usually `~/go/bin`) is in your `PATH`.

## Building

```bash
make
```

This runs `go generate` (to produce the plugin registration boilerplate) then builds `build/libgsttvproxymux.so`.

To verify the plugin loads:

```bash
make inspect
```

Expected output includes:

```
Pad Templates:
  SINK template: 'video'
    Availability: On request
    Capabilities:
      video/x-h264
      video/x-h265
      video/mpeg
            mpegversion: 2

  SINK template: 'audio'
    Availability: On request
    Capabilities:
      audio/mpeg
            mpegversion: { (int)2, (int)4 }
      audio/x-ac3
      audio/x-eac3

  SRC template: 'src'
    Availability: Always
    Capabilities:
      video/quicktime
      video/mpegts
```

## Usage

Set `GST_PLUGIN_PATH` to the build directory so GStreamer can find the plugin:

```bash
export GST_PLUGIN_PATH=./build
```

### Video only (h264 to MP4)

```bash
gst-launch-1.0 -e \
  videotestsrc is-live=true num-buffers=150 \
  ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 \
  ! tvproxymux name=m ! filesink location=out.mp4
```

### Video + audio (h264 + AAC to MP4)

```bash
gst-launch-1.0 -e \
  videotestsrc is-live=true num-buffers=150 \
  ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 ! m.video \
  audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
  tvproxymux name=m ! filesink location=out.mp4
```

### h265 video + audio to MP4

```bash
gst-launch-1.0 -e \
  videotestsrc is-live=true num-buffers=150 \
  ! "video/x-raw,format=I420,width=640,height=480,framerate=25/1" \
  ! vtenc_h265 bitrate=2000 realtime=true allow-frame-reordering=false ! m.video \
  audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
  tvproxymux name=m ! filesink location=out.mp4
```

### MPEG-TS output

```bash
gst-launch-1.0 -e \
  videotestsrc is-live=true num-buffers=150 \
  ! x264enc speed-preset=ultrafast bitrate=2000 key-int-max=30 ! m.video \
  audiotestsrc is-live=true num-buffers=150 ! faac ! m.audio \
  tvproxymux name=m output-format=mpegts ! filesink location=out.ts
```

### With tvproxydemux (live HDHR/SAT>IP source)

```bash
# Browser-ready MP4:
gst-launch-1.0 -e \
  souphttpsrc location=http://hdhr:5004/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! tvproxymux name=m ! filesink location=out.mp4 \
  d.audio ! m.

# MPEG-TS for VLC/Plex/Jellyfin:
gst-launch-1.0 -e \
  souphttpsrc location=http://hdhr:5004/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! tvproxymux name=m output-format=mpegts ! filesink location=out.ts \
  d.audio ! m.
```

### With h265 transcode

```bash
gst-launch-1.0 -e \
  souphttpsrc location=http://hdhr:5004/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! vtdec ! vtenc_h265 bitrate=4000 realtime=true allow-frame-reordering=false \
  ! tvproxymux name=m ! filesink location=out.mp4 \
  d.audio ! m.
```

## Integration guide

This section is for agents or developers wiring `tvproxymux` into a larger pipeline programmatically using Go and go-gst.

### Linking pads

`tvproxymux` uses request pads. You must explicitly request them by name:

```go
mux, _ := gst.NewElement("tvproxymux")
mux.SetProperty("output-format", "mp4") // or "mpegts"

// Request pads by name
videoPad := mux.GetRequestPad("video")
audioPad := mux.GetRequestPad("audio")

// Link upstream elements to these pads
videoEncoder.GetStaticPad("src").Link(videoPad)
audioParser.GetStaticPad("src").Link(audioPad)

// The src pad is always present
mux.GetStaticPad("src").Link(downstream.GetStaticPad("sink"))
```

### Connecting to a demuxer with dynamic pads

When connecting to a demuxer that emits pads dynamically (like `tsdemux` or `tvproxydemux`), use the `pad-added` signal:

```go
demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
    caps := pad.GetCurrentCaps()
    if caps == nil {
        return
    }
    name := caps.GetStructureAt(0).Name()
    if strings.HasPrefix(name, "video") {
        pad.Link(mux.GetRequestPad("video"))
    } else if strings.Contains(name, "audio") {
        pad.Link(mux.GetRequestPad("audio"))
    }
})
```

### Key constraints

- Request the `video` pad **once**. A second request returns nil.
- Request the `audio` pad **once**. A second request returns nil.
- Set `output-format` **before** requesting any pads. The muxer element is created on the first pad request and the format cannot be changed after that.
- Set `fragment-duration` **before** requesting any pads (only affects MP4 mode).
- The element handles codec detection internally. Do not insert your own parser between the encoder/demuxer and tvproxymux -- it will double-parse.

### What the element does not do

- It does not handle sources (HTTP, RTSP, file). Use `souphttpsrc`, `rtspsrc`, `filesrc`, etc.
- It does not demux. Use `tsdemux`, `tvproxydemux`, etc.
- It does not decode or encode. Use `vtdec`/`vtenc_h265`/`x264enc`, etc.
- It does not handle sinks (file, network, HLS). Use `filesink`, `hlssink2`, etc.

It only handles the muxing step: parser insertion, muxer configuration, and container output.

## Hardcoded settings reference

These settings are baked into the plugin and are not configurable (by design). They were validated against live HDHR and SAT>IP streams.

### mp4mux (MP4 mode)

| Setting | Value | Reason |
|---------|-------|--------|
| `fragment-duration` | 500 (default, configurable 100-1000) | 500ms fragments give ~3.4s first byte |
| `streamable` | `true` | File is playable while still being written |

Do **not** use `isofmp4mux` -- it buffers internally and takes 12 seconds to first byte.

### mpegtsmux (MPEG-TS mode)

All default settings. No properties are set. Do **not** set `latency`, `alignment`, or any buffering properties -- they all break video output.

### h264parse / h265parse

| Setting | Value | Reason |
|---------|-------|--------|
| `config-interval` | `-1` | Insert VPS/SPS/PPS on every keyframe so players can start decoding at any point |

### mpegvideoparse (MPEG-2)

No special settings. `config-interval` is not applicable to MPEG-2 parsers.

## Running tests

```bash
make test-step2   # h264 video only -> MP4
make test-step3   # h264 video + AAC audio -> MP4
make test-step4   # h265 video + AAC audio -> MP4 (requires VideoToolbox on macOS)
make test-step5   # h264 video + AAC audio -> MPEG-TS
```

Each test produces an output file in `/tmp/` and runs `ffprobe` to verify the stream contents. Expected results:

| Test | Expected ffprobe output |
|------|------------------------|
| step2 | `codec_name=h264\|codec_type=video` |
| step3 | `codec_name=h264\|codec_type=video` and `codec_name=aac\|codec_type=audio` |
| step4 | `codec_name=hevc\|codec_type=video` and `codec_name=aac\|codec_type=audio` |
| step5 | `codec_name=h264\|codec_type=video` and `codec_name=aac\|codec_type=audio` (in MPEG-TS) |

## Project structure

```
tvproxymux/
  tvproxymux.go              # Plugin implementation
  zzgenerated_plugin.go      # Auto-generated plugin registration (do not edit)
  go.mod / go.sum            # Go module files
  Makefile                   # Build and test targets
  CLAUDE.md                  # Development notes and design rationale
  reference/
    manual_mux.go            # Reference: the manual pipeline this plugin replaces
  build/
    libgsttvproxymux.so      # Compiled plugin (created by make)
```

## License

LGPL (matches GStreamer plugin licensing conventions).
