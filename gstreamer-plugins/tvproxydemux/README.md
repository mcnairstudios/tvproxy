# tvproxydemux — GStreamer MPEG-TS Demux Plugin

A GStreamer element that demuxes MPEG-TS streams and auto-transcodes audio to stereo AAC, exposing static `video` and `audio` source pads. Designed for live TV streaming pipelines where you need a simple, reliable pipeline string instead of programmatic `pad-added` signal handlers.

## Why This Exists

GStreamer's stock `tsdemux` element creates dynamic pads at runtime, which makes it impossible to build working MPEG-TS pipelines using pipeline strings (e.g. `gst_parse_launch()` or `NewPipelineFromString()` in go-gst). Every alternative has a fatal flaw:

| Approach | Problem |
|----------|---------|
| `tsdemux ! queue ! avdec_aac_latm` | Audio pad never created — "Delayed linking failed" |
| `tsdemux` + `decodebin` | Pipeline deadlocks when a muxer waits for both streams |
| `parsebin` | Works but adds 3-7 seconds of typefinding latency |
| Native `pad-added` handler | Works fast (2.7-3.4s) but requires programmatic construction |

`tvproxydemux` gives you the speed of the native `pad-added` approach with the simplicity of a pipeline string:

```
souphttpsrc location=http://hdhr/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! h264parse config-interval=-1 ! mp4mux name=mux fragment-duration=500 streamable=true \
  ! filesink location=out.mp4 \
  d.audio ! mux.
```

First byte in **3.1 seconds** with mp4mux. Versus 7-11 seconds with `parsebin`.

## How It Works

`tvproxydemux` is a `GstBin` subclass containing `tsparse`, `tsdemux`, codec parsers, audio decoders, and encoders wired internally. It exposes three static (always-on) pads:

```
                        +---------------------------+
   video/mpegts ------->| sink                      |
                        |   tsparse -> tsdemux      |
                        |     |-> parser -> [video]  |-------> parsed video
                        |     |-> transcode -> [audio]|-------> AAC audio
                        +---------------------------+
```

- **sink** — accepts `video/mpegts`
- **video** — outputs parsed video (`video/x-h264`, `video/x-h265`, or `video/mpeg`)
- **audio** — outputs AAC audio (`audio/mpeg, mpegversion=4`)

Pads exist from element construction time, so downstream elements can link during pipeline parsing — before any data flows. This is the key difference from `tsdemux`, which only creates pads after it sees data.

### Internal auto-detection

When data arrives, `tsdemux` fires `pad-added` internally. The plugin detects the codec from pad caps and builds the correct chain:

**Video** (auto-detected from caps):
- `video/x-h264` -> `h264parse`
- `video/x-h265` -> `h265parse`
- `video/mpeg` -> `mpegvideoparse`

**Audio** (auto-detected, transcoded to stereo AAC):
- AAC-LATM: `aacparse -> avdec_aac_latm -> audioconvert -> audioresample -> capsfilter(ch=2) -> faac -> aacparse`
- AC3/EAC3: `avdec_ac3 -> audioconvert -> audioresample -> capsfilter(ch=2) -> faac -> aacparse`
- MP2/MP3: `mpegaudioparse -> mpg123audiodec -> audioconvert -> audioresample -> capsfilter(ch=2) -> faac -> aacparse`
- Plain AAC: `aacparse` (passthrough)

Critical implementation detail: `aacparse` MUST precede `avdec_aac_latm`, and `mpegaudioparse` MUST precede `mpg123audiodec`. Without these parsers, the decoders receive unparsed frames and produce zero output.

### Unused stream handling

MPEG-TS streams often contain multiple audio tracks (e.g. stereo + surround, or multiple languages) and subtitle tracks. The plugin links only one video and one audio stream. All other pads from `tsdemux` are automatically connected to internal `fakesink` elements to prevent `not-negotiated` flow errors from killing the pipeline.

## Properties

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `audio-channels` | int (1-8) | `2` | Target audio channel count for transcode |
| `audio-codec` | string | `"aac"` | `"aac"` to transcode, `"copy"` for passthrough (only works with plain AAC streams; LOAS/AC3/MP2 auto-falls back to transcode) |
| `video-codec-hint` | string | `""` | Force video parser: `"h264"`, `"h265"`, `"mpeg2"`, or empty for auto-detect |
| `audio-language` | string | `""` | Preferred audio language as ISO 639 code (e.g. `"eng"`). Empty = first non-AD track. When set, collects all audio pads and selects the best match at `no-more-pads`. |
| `video-interlaced` | bool | `false` | Read-only. `TRUE` if the source video is interlaced, detected from stream caps. |

## Building

Requires GStreamer development libraries (>= 1.20) and Meson.

```bash
# macOS (Homebrew)
brew install gstreamer meson

# Build
meson setup build
meson compile -C build

# Verify
GST_PLUGIN_PATH=./build gst-inspect-1.0 tvproxydemux
```

Produces `build/gsttvproxydemux.dylib` (macOS) or `build/gsttvproxydemux.so` (Linux).

### GStreamer element dependencies

The following GStreamer elements must be installed (all standard in Homebrew's `gstreamer` package):

- `tsparse`, `tsdemux` (mpegtsdemux plugin)
- `h264parse`, `h265parse` (videoparsersbad plugin)
- `mpegvideoparse` (videoparsersbad plugin)
- `aacparse`, `mpegaudioparse` (audioparsers plugin)
- `avdec_aac_latm`, `avdec_ac3`, `avdec_eac3` (libav plugin)
- `mpg123audiodec` (mpg123 plugin)
- `faac` (faac plugin)
- `audioconvert`, `audioresample` (audioconvert, audioresample plugins)

## Usage Examples

### Basic: MPEG-TS source to MP4 file
```bash
GST_PLUGIN_PATH=./build gst-launch-1.0 -e \
  souphttpsrc location=http://192.168.1.186:5004/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! h264parse config-interval=-1 \
  ! mp4mux name=mux fragment-duration=500 streamable=true \
  ! filesink location=/tmp/output.mp4 \
  d.audio ! mux.
```

### Hardware transcode: h264 to h265 via VideoToolbox (macOS)
```bash
GST_PLUGIN_PATH=./build gst-launch-1.0 -e \
  souphttpsrc location=http://192.168.1.186:5004/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! vtdec ! vtenc_h265 bitrate=4000 realtime=true allow-frame-reordering=false \
  ! h265parse config-interval=-1 \
  ! mpegtsmux name=mux ! filesink location=/tmp/output.ts \
  d.audio ! mux.
```

### MPEG-TS output with mpeg2 source (e.g. SD Freeview)
```bash
GST_PLUGIN_PATH=./build gst-launch-1.0 -e \
  souphttpsrc location=http://192.168.1.186:5004/auto/v38 do-timestamp=true is-live=true \
  ! tvproxydemux name=d \
  d.video ! mpegvideoparse \
  ! mpegtsmux name=mux ! filesink location=/tmp/output.ts \
  d.audio ! mux.
```

### With audio language preference
```bash
GST_PLUGIN_PATH=./build gst-launch-1.0 -e \
  souphttpsrc location=http://hdhr/auto/v101 do-timestamp=true is-live=true \
  ! tvproxydemux audio-language=eng name=d \
  d.video ! h264parse config-interval=-1 \
  ! mpegtsmux name=mux ! filesink location=/tmp/output.ts \
  d.audio ! mux.
```

## Integration with Go (go-gst)

The plugin is loaded via `GST_PLUGIN_PATH` and used as a normal element in pipeline strings:

```go
import "github.com/go-gst/go-gst/gst"

func main() {
    gst.Init(nil)

    // Set GST_PLUGIN_PATH before calling gst.Init, or:
    // os.Setenv("GST_PLUGIN_PATH", "/path/to/build")

    pipelineStr := fmt.Sprintf(
        "souphttpsrc location=%s do-timestamp=true is-live=true "+
            "! tvproxydemux name=d "+
            "d.video ! h264parse config-interval=-1 "+
            "! mp4mux name=mux fragment-duration=500 streamable=true "+
            "! filesink location=%s "+
            "d.audio ! mux.",
        url, outputPath)

    pipeline, err := gst.NewPipelineFromString(pipelineStr)
    if err != nil {
        log.Fatal(err)
    }

    pipeline.SetState(gst.StatePlaying)
    // ...
}
```

### Before/after comparison for tvproxy integration

```go
// BEFORE: parsebin (works but 7-11 second startup)
"souphttpsrc location=%s ! parsebin name=demux " +
    "demux. ! video/x-h264 ! queue ! h264parse ! mux. " +
    "mp4mux name=mux ! filesink location=%s " +
    "demux. ! audio/mpeg ! queue ! aacparse ! avdec_aac_latm " +
    "! audioconvert ! audioresample ! audio/x-raw,channels=2 ! faac ! aacparse ! mux."

// AFTER: tvproxydemux (3.1 second startup, handles all codecs)
"souphttpsrc location=%s do-timestamp=true is-live=true " +
    "! tvproxydemux name=d " +
    "d.video ! h264parse config-interval=-1 " +
    "! mp4mux name=mux fragment-duration=500 streamable=true " +
    "! filesink location=%s " +
    "d.audio ! mux."
```

### Reading the interlaced flag from Go

```go
// After pipeline is playing and streams are detected:
demux, _ := pipeline.GetByName("d")
interlaced, _ := demux.GetProperty("video-interlaced")
if interlaced.(bool) {
    // Source is interlaced — consider deinterlacing downstream
}
```

## Tested Configurations

| Source | Video | Audio | Output | Result |
|--------|-------|-------|--------|--------|
| HDHomeRun v101 (BBC ONE) | h264 | AAC-LATM | mp4mux | 3.1s first byte |
| HDHomeRun v101 (BBC ONE) | h264 | AAC-LATM | mpegtsmux | 3.1s first byte |
| HDHomeRun v38 (Channel 5+1) | mpeg2 | MP2 | mpegtsmux | Working |
| HDHomeRun v101 | h264->h265 (vtdec/vtenc) | AAC-LATM | mpegtsmux | Working |
| HDHomeRun v101-v105 | various | various | fakesink | 5 channels, no hangs |

## Architecture

### File structure
```
src/
  gsttvproxydemux.h    # Type declarations, struct definition
  gsttvproxydemux.c    # All logic: init, pad-added, chain building
  plugin.c             # GST_PLUGIN_DEFINE registration
meson.build            # Build configuration
```

### Key design decisions

1. **Queue-based ghost pads**: Internal `queue` elements provide stable ghost pad targets at construction time. The queues exist before data flows, so downstream can link during pipeline parsing. When `tsdemux` fires `pad-added`, parser/decoder chains are built and connected to the queue sink pads.

2. **Fakesink for unused pads**: MPEG-TS streams often have multiple audio tracks and subtitle streams. Unlinked `tsdemux` pads cause `not-negotiated` flow errors that propagate upstream and kill the pipeline. Every unused pad gets an internal `fakesink` to absorb data silently.

3. **Audio language selection**: When `audio-language` is set, the plugin defers audio linking until `tsdemux` signals `no-more-pads`, then selects the best match. Audio description (AD) tracks are deprioritized in all cases.

4. **Copy mode fallback**: `audio-codec=copy` only passes through plain AAC (ADTS/raw). For AAC-LATM, AC3, or MP2 streams, it automatically falls back to transcoding because muxers cannot accept those formats directly.
