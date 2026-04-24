# Changes Required in tvproxy (outside pkg/lib/av)

## DELIVERED — Ready for tvproxy to use

```go
// Audio encoder — takes codec string from OutputAudioCodec, auto-resolves encoder name
enc, err := encode.NewAudioEncoder(encode.AudioEncodeOpts{
    Codec:      opts.OutputAudioCodec,  // "aac", "opus", "mp3", "flac", etc.
    Channels:   2,
    SampleRate: 48000,
})

// AudioFIFO — reads frame size from the encoder (varies by codec)
fifo := encode.NewAudioFIFOFromEncoder(enc, 2, astiav.ChannelLayoutStereo, 48000)

// Decoders — auto-detect everything from CodecParameters
videoDec, err := decode.NewVideoDecoderFromParams(dm.VideoCodecParameters(), opts)
audioDec, err := decode.NewAudioDecoderFromParams(dm.AudioCodecParameters())

// Codec string from init segment — H.264 and HEVC
muxer.VideoCodecString()  // "avc1.42C01E" or "hev1.1.L120.90"

// Muxer seek reset — flush partial fragment, reset DTS tracking
muxer.Reset()

// AudioFIFO seek reset — discard stale samples
audioFifo.Reset()

// Cached probe for fast reconnect (skip FindStreamInfo)
dm, err := demux.NewDemuxer(url, demux.DemuxOpts{CachedStreamInfo: cachedInfo})
```

## In-Place Seek — How to Wire It

Based on analysis of ffmpeg's own seek implementation. Key principles:

1. **Never restart the stream.** Seek happens in-place via `demuxer.RequestSeek(posMs)`.
2. **PTS is movie time.** Seek to 3120s = packets have PTS at 3120s+. No rebasing.
3. **No decoder flush needed.** ffmpeg itself doesn't call `avcodec_flush_buffers` on seek — it just feeds new packets.

### onSeek Callback

Register via `demuxer.SetOnSeek(fn)`. The callback fires on the read goroutine between ReadFrame calls. It should:

```go
demuxer.SetOnSeek(func() {
    muxer.Reset()           // flush partial fragment, reset DTS tracking
    audioFifo.Reset()       // discard stale pre-seek samples
    audioLatched = false    // clear audio error latch
    watcher.Reset()         // bump generation → frontend restarts fetch
})
```

### seekFunc Registration

```go
s.SetSeekFunc(func(position float64) {
    seekMs := int64(position * 1000)
    demuxer.RequestSeek(seekMs)  // thread-safe, runs on read goroutine
})
```

### Accurate Seek — Trim Pre-Target Packets

ffmpeg uses a trim filter to discard frames between the nearest keyframe and the actual seek target. For accurate positioning, the pipeline should:

1. Store the seek target position when `RequestSeek` is called
2. In `PushVideo`/`PushAudio`, drop packets with PTS < seekTarget
3. This avoids showing frames from before the requested position

Without trimming, seek to 60s might show a flash of content from 58s (the nearest preceding keyframe). With trimming, playback starts exactly at 60s.

This is optional — Chrome MSE handles the slight pre-seek frames gracefully. But for a polished experience, implement the trim.

## Seek — Frontend SourceBuffer Setup

After seek, the av package produces segments with movie-time PTS (e.g., seek to 2678s = segments at PTS 2678s). Chrome's `media-internals` shows `pipeline_state: kSeeking` stuck — never reaches `kPlaying`.

**Root cause**: Chrome MSE SourceBuffer has data at 0-67s from pre-seek. After seek, new init + data arrives at PTS 2678s. Chrome can't bridge the gap.

**Frontend fix**: After seek, when creating the new MediaSource/SourceBuffers:
1. Set `mediaSource.duration` to the movie duration
2. Call `video.currentTime = seekPosition` AFTER appending the first post-seek segment
3. Chrome will then find data at 2678s and start playing

Or alternatively: set `sourceBuffer.timestampOffset` to shift the segments, but movie-time PTS is cleaner — just make sure `currentTime` matches.

The av package segments are correct — movie-time PTS at the seek position.

## REQ-022: Wire OutputAudioCodec Through Pipelines

tvproxy passes `OutputAudioCodec` (e.g. "aac", "opus", "copy") in pipeline opts.

**tvproxy needs to:**
1. Pass `opts.OutputAudioCodec` directly to `encode.NewAudioEncoder`. The av library resolves the encoder name internally.
2. Use `encode.NewAudioFIFOFromEncoder` instead of `encode.NewAudioFIFO` with hardcoded frame sizes.
3. When `OutputAudioCodec == "copy"` or empty → passthrough. Anything else → decode + resample + encode.

## Fast Reconnect — CachedStreamInfo

For SAT>IP/RTSP live sources, first tune takes ~5-7s (tuner lock + stream analysis). Subsequent tunes to the same channel can skip `FindStreamInfo` by passing cached probe data:

```go
// First tune: full probe
dm, _ := demux.NewDemuxer(url, demux.DemuxOpts{...})
info := dm.StreamInfo()  // cache this

// Next tune: skip FindStreamInfo (~200ms open)
dm2, _ := demux.NewDemuxer(url, demux.DemuxOpts{
    CachedStreamInfo: info,
})
```

The cached `StreamInfo` provides stream indices and codec metadata. The demuxer uses them directly without waiting for PAT/PMT analysis. Invalidate the cache on stream errors.
