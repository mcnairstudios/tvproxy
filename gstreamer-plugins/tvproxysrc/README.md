# tvproxysrc

A GStreamer source bin plugin written in Go that unifies HTTP, RTSP, and file MPEG-TS inputs behind a single element with a static source pad.

## Why this exists

Live MPEG-TS streams arrive via three different transports, and each one requires different GStreamer elements, properties, and linking strategies:

| Transport | Elements needed | Complexity |
|-----------|----------------|------------|
| HTTP (HDHomeRun, IPTV) | `souphttpsrc` with `do-timestamp=true is-live=true` | Low, but easy to forget the timestamp properties |
| RTSP (SAT>IP DVB-T/T2/S/S2) | `rtspsrc` + `rtpmp2tdepay` with `pad-added` signal handler | High -- dynamic pads can't be expressed in pipeline strings |
| File (recordings, test files) | `filesrc` | Trivial |

Without this plugin, every caller (session manager, test scripts, CLI pipelines) must:

1. Detect the protocol from the URL
2. Create the correct source element(s) with the correct properties
3. For RTSP: write a `pad-added` signal handler to link `rtspsrc` to `rtpmp2tdepay` at runtime

The RTSP case is the real pain point. `rtspsrc` has dynamic pads, which means you can't express a pipeline with it as a simple string. You either write imperative Go/C code to handle `pad-added`, or use `parsebin`/`decodebin` which adds 3-7 seconds of overhead.

`tvproxysrc` eliminates all of this. One element, one `location` property, works everywhere:

```
tvproxysrc location=http://...  -->  raw MPEG-TS on static src pad
tvproxysrc location=rtsp://...  -->  raw MPEG-TS on static src pad
tvproxysrc location=/path/...   -->  raw MPEG-TS on static src pad
```

## How it works

`tvproxysrc` is a GstBin subclass that:

1. Exposes a **static** ghost source pad (`src`) with `video/mpegts` caps from construction time
2. When `location` is set, auto-detects the protocol from the URL scheme
3. Creates the correct internal element chain and links it to the ghost pad

### Internal pipelines

**HTTP mode** (`http://` or `https://`):
```
souphttpsrc location={url} do-timestamp=true is-live=true --> [ghost src pad]
```

**RTSP mode** (`rtsp://` or `rtsps://`):
```
rtspsrc location={url} latency=0 protocols=TCP --(pad-added)--> rtpmp2tdepay --> [ghost src pad]
```
The `pad-added` handler runs inside the bin. Callers never see it.

**File mode** (everything else):
```
filesrc location={path} --> [ghost src pad]
```

### The static pad guarantee

The source pad exists from element construction, before any state change. This means `tvproxysrc` works in `gst-launch-1.0` pipeline strings and can be linked directly to downstream elements:

```bash
gst-launch-1.0 tvproxysrc location=rtsp://... ! tsdemux ! h264parse ! filesink location=out.ts
```

No `pad-added` callbacks, no `parsebin`, no protocol switching logic.

## Properties

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `location` | string | `null` | URL or file path. Protocol is detected from the scheme. |
| `is-live` | bool | `true` | For HTTP mode: sets `do-timestamp` and `is-live` on `souphttpsrc`. Set to `false` for file playback. |
| `rtsp-transport` | string | `"tcp"` | `"tcp"` or `"udp"`. TCP is the default because UDP drops packets on congested networks. |

## Building

### Prerequisites

- Go 1.21+
- GStreamer 1.20+ development libraries
- `pkg-config` must find `gstreamer-1.0`

On macOS:
```bash
brew install gstreamer
```

On Debian/Ubuntu:
```bash
apt install libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
            gstreamer1.0-plugins-good gstreamer1.0-plugins-bad
```

The `gstreamer1.0-plugins-good` package provides `souphttpsrc` and `rtspsrc`. The `gstreamer1.0-plugins-bad` package provides `tsdemux`/`tsparse`.

### Build steps

```bash
# Install the code generator (one time)
go install github.com/go-gst/go-gst/cmd/gst-plugin-gen@latest

# Generate the plugin boilerplate
go generate

# Build the shared library
mkdir -p build
go build -o build/libgsttvproxysrc.so -buildmode c-shared .
```

### Verify

```bash
GST_PLUGIN_PATH=./build gst-inspect-1.0 tvproxysrc
```

Expected output includes:
```
Klass:       Source/Bin
Pad Templates:
  SRC template: 'src'
    Availability: Always
    Capabilities: video/mpegts
```

## Usage

Set `GST_PLUGIN_PATH` to the directory containing `libgsttvproxysrc.so`, then use `tvproxysrc` like any other GStreamer element.

### gst-launch-1.0 examples

```bash
# HTTP -- HDHomeRun tuner
gst-launch-1.0 -e \
  tvproxysrc location=http://192.168.1.186:5004/auto/v101 \
  ! tsparse set-timestamps=true ! tsdemux ! h264parse \
  ! mpegtsmux ! filesink location=output.ts

# RTSP -- SAT>IP DVB-T2 tuner
gst-launch-1.0 -e \
  tvproxysrc location="rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&mtype=256qam&pids=0,6650,6601,6602,6606,6605&bw=8&plp=0" \
  ! tsparse set-timestamps=true ! tsdemux ! h264parse \
  ! mpegtsmux ! filesink location=output.ts

# File -- local recording
gst-launch-1.0 -e \
  tvproxysrc location=/tmp/recording.ts is-live=false \
  ! tsparse set-timestamps=true ! tsdemux ! h264parse \
  ! mpegtsmux ! filesink location=output.ts
```

### Go integration

```go
import "github.com/go-gst/go-gst/gst"

// Create the element
src, _ := gst.NewElement("tvproxysrc")
src.SetProperty("location", "rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&...")

// Link it like any other element with a static src pad
pipeline.Add(src, nextElement)
src.Link(nextElement)

// That's it. No pad-added handler, no protocol detection.
pipeline.SetState(gst.StatePlaying)
```

Compare this with what you'd need without the plugin for RTSP:

```go
src, _ := gst.NewElement("rtspsrc")
src.SetProperty("location", url)
src.SetProperty("latency", uint(0))
src.SetProperty("protocols", uint(4))
depay, _ := gst.NewElement("rtpmp2tdepay")

// Must handle dynamic pads manually
src.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
    sinkPad := depay.GetStaticPad("sink")
    if sinkPad != nil && !sinkPad.IsLinked() {
        pad.Link(sinkPad)
    }
})

pipeline.AddMany(src, depay, nextElement)
gst.ElementLinkMany(depay, nextElement)
```

### With companion plugins

`tvproxysrc` is designed to work with two other tvproxy plugins:

- **`tvproxydemux`** -- demuxes MPEG-TS into separate video and audio pads
- **`tvproxymux`** -- muxes video and audio into fragmented MP4

```bash
# Full pipeline: any source -> demux -> mux -> file
gst-launch-1.0 -e \
  tvproxysrc location={any_url} \
  ! tvproxydemux name=d \
  d.video ! tvproxymux name=m ! filesink location=output.mp4 \
  d.audio ! m.
```

## Protocol detection

Detection is based on the URL scheme prefix:

| Prefix | Mode | Internal elements |
|--------|------|-------------------|
| `http://` or `https://` | HTTP | `souphttpsrc` |
| `rtsp://` or `rtsps://` | RTSP | `rtspsrc` + `rtpmp2tdepay` |
| Anything else | File | `filesrc` |

## RTSP details

### SAT>IP URL format

SAT>IP devices use RTSP with tuning parameters in the query string:

```
rtsp://{host}/?freq={mhz}&msys={system}&mtype={modulation}&pids={pid_list}&bw={bandwidth}&plp={plp_id}
```

The plugin passes the URL to `rtspsrc` as-is. The SAT>IP device interprets the tuning parameters.

### Transport and latency

- **TCP** (`protocols=4`) is the default because it's more reliable for live TV than UDP
- **Latency** is set to 0 to minimize buffering for live playback
- To use UDP instead: `tvproxysrc location=rtsp://... rtsp-transport=udp`

### Dynamic pad handling

`rtspsrc` creates pads dynamically after RTSP SETUP completes. Inside the bin, a `pad-added` signal handler links `rtspsrc`'s dynamic pad to `rtpmp2tdepay`'s static sink pad. The ghost pad targets `rtpmp2tdepay`'s static src pad, so the external source pad is always available.

## Performance

Tested 2026-04-11 against real hardware:

| Transport | Source | First byte latency | Notes |
|-----------|--------|-------------------|-------|
| HTTP | HDHomeRun | ~1.7s | Tuner already locked |
| RTSP | SAT>IP DVB-T2 545MHz | 11-12s | DVB-T2 tuner lock is 5-8s |
| RTSP | SAT>IP DVB-T 490MHz | 8-10s | DVB-T tuner lock is 3-5s |
| File | Local .ts file | <0.5s | Disk I/O only |

The plugin adds zero measurable overhead versus using the raw GStreamer elements directly (tested: raw `souphttpsrc` 1.74s vs `tvproxysrc` 1.71s for first byte).

## Output

The source pad always outputs raw `video/mpegts` bytes regardless of transport. This is the raw MPEG-TS transport stream -- it has not been demuxed. To access individual video/audio streams, pipe into `tsdemux` or `tvproxydemux`.

## Project structure

```
tvproxysrc.go            # Plugin implementation
zzgenerated_plugin.go    # Auto-generated by gst-plugin-gen (do not edit)
go.mod / go.sum          # Go module
build/                   # Build output (libgsttvproxysrc.so)
reference/               # Working Go programs showing what the plugin replaces
  reference_http.go      # Raw souphttpsrc usage with HDHomeRun
  reference_rtsp.go      # Raw rtspsrc + rtpmp2tdepay with SAT>IP
```

## License

LGPL (matches GStreamer plugin licensing conventions).
