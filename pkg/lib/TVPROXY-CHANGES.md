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
```

## REQ-022: Wire OutputAudioCodec Through Pipelines

tvproxy passes `OutputAudioCodec` (e.g. "aac", "opus", "copy") in pipeline opts.

**tvproxy needs to:**
1. Pass `opts.OutputAudioCodec` directly to `encode.NewAudioEncoder`. The av library resolves the encoder name internally.
2. Use `encode.NewAudioFIFOFromEncoder` instead of `encode.NewAudioFIFO` with hardcoded frame sizes.
3. When `OutputAudioCodec == "copy"` or empty → passthrough. Anything else → decode + resample + encode.

## REQ-023: FIXED in av package

Max-duration video flush + PCM S16LE audio. No tvproxy changes needed.

## REQ-025: FIXED in av package

H.264 codec_string extraction. No tvproxy changes needed.

## REQ-021: Already handled

WG proxy already copies upstream Content-Type headers.
