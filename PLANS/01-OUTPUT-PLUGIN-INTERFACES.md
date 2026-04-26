# Output Plugin Interfaces

## Package: pkg/output/

### Core Interface

```go
type DeliveryMode string // "mse", "hls", "stream", "dash", "webrtc", "record"

type OutputPlugin interface {
    Mode() DeliveryMode
    PushVideo(data []byte, pts, dts int64, keyframe bool) error
    PushAudio(data []byte, pts, dts int64) error
    PushSubtitle(data []byte, pts int64, duration int64) error
    EndOfStream()
    ResetForSeek()
    Stop()
    Status() PluginStatus
}
```

### ServablePlugin (for HTTP-served delivery)

```go
type ServablePlugin interface {
    OutputPlugin
    ServeHTTP(w http.ResponseWriter, r *http.Request)
    Generation() int64
    WaitReady(ctx context.Context) error
}
```

### FanOut (one decode → N outputs)

```go
type FanOut struct { plugins []OutputPlugin }

func (f *FanOut) PushVideo(...)  // sends to all plugins, errors don't kill others
func (f *FanOut) PushAudio(...)  // same
func (f *FanOut) Add(p OutputPlugin)      // attach mid-stream (recording starts)
func (f *FanOut) Remove(mode DeliveryMode) // detach (recording stops)
```

### DecodeBridge (transcode layer)

```go
type DecodeBridge struct {
    downstream PacketSink  // FanOut
    videoDec, audioDec, videoEnc, audioEnc, deint, scaler, audioFifo
}
// Implements PacketSink: receives compressed, outputs re-encoded
```

### Architecture

```
Copy mode:    Demuxer → FanOut → [MSE, Recording, ...]
Transcode:    Demuxer → DecodeBridge → FanOut → [MSE, Recording, ...]
```

### Concrete Plugins

| Plugin | Implements | Replaces |
|--------|-----------|----------|
| MSEPlugin | OutputPlugin + ServablePlugin | MSECopy + MSETranscode pipelines |
| HLSPlugin | OutputPlugin + ServablePlugin | HLSCopy + HLSTranscode pipelines |
| StreamPlugin | OutputPlugin | StreamCopy + FullTranscode pipelines |
| RecordingPlugin | OutputPlugin | Recording consumer hack |

Six pipeline types → three plugins + one DecodeBridge.

### Session Manager Simplification

Six `runGoXxx` methods → one `runPipeline`:
1. Resolve client profile
2. Create output plugins from registry
3. Create FanOut
4. Optionally create DecodeBridge
5. Run demuxer with sink chain

### File Layout

```
pkg/output/
    plugin.go, config.go, registry.go, fanout.go, bridge.go, profile.go
    mse/mse.go
    hls/hls.go
    stream/stream.go
    record/record.go
```
