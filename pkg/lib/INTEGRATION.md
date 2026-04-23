# AV Library Integration Notes

How tvproxy should use the av library.

## Audio Input — Fully Auto-Detected

The av library auto-detects all audio input formats. tvproxy does NOT need to specify the input format, sample rate, channel layout, or bit depth. The probe identifies stream indices and provides `CodecParameters`. After that, libavformat handles everything:

- Decoder: initialized from `CodecParameters` via `NewAudioDecoderFromParams(cp)`
- Resampler: auto-negotiates source format from each input frame (any format, any channels, any rate)
- AudioFIFO: buffers to the encoder's required frame size
- Encoder: produces output in the requested codec

tvproxy only needs to specify: **which audio stream** (index from probe) and **what output codec**.

## Audio Output — Codec String from tvproxy

tvproxy passes the output audio codec as a string. The av library encodes to that format.

### Transcode Options

| Codec String | Encoder Name | Description |
|-------------|-------------|-------------|
| `aac` | `aac` | AAC-LC. Default for browser MSE. |
| `opus` | `libopus` | Opus. WebM, low-latency streaming. |
| `mp3` | `libmp3lame` | MP3. Legacy compatibility. |
| `mp2` | `mp2` | MPEG Audio Layer 2. DVB standard. |
| `ac3` | `ac3` | Dolby Digital. DVD/broadcast. |
| `eac3` | `eac3` | Dolby Digital Plus. Streaming services. |
| `flac` | `flac` | FLAC lossless. Archival. |
| `vorbis` | `libvorbis` | Vorbis. WebM/Ogg. |

### Copy (Passthrough)

When output is `copy`, the audio stream is passed through untouched. No decode, no resample, no encode. The original codec is preserved (DTS, TrueHD, FLAC, etc.). This is used for DLNA/Jellyfin clients that support the source codec natively.

## API

### Creating the Audio Encoder

```go
enc, err := encode.NewAudioEncoder(encode.AudioEncodeOpts{
    Codec:      "aac",      // encoder name from table above
    Channels:   2,          // output channel count
    SampleRate: 48000,      // output sample rate
})

// Frame size comes from the encoder (AAC=1024, MP3=1152, Opus=960, etc.)
frameSize := enc.FrameSize()
```

### Creating the Audio Pipeline

```go
// 1. Decoder from CodecParameters (auto-detects everything)
audioDec, err := decode.NewAudioDecoderFromParams(dm.AudioCodecParameters())

// 2. Resampler — always created, auto-negotiates source from input frames
audioResample, err := resample.NewResampler(
    srcChannels, srcRate, astiav.SampleFormatFltp,  // source hints (not enforced)
    outChannels, outRate, astiav.SampleFormatFltp,   // destination (enforced)
)

// 3. Encoder for the requested output codec
audioEnc, err := encode.NewAudioEncoder(encode.AudioEncodeOpts{
    Codec: outputCodec, Channels: outChannels, SampleRate: outRate,
})

// 4. FIFO buffers to encoder's frame size
audioFifo := encode.NewAudioFIFOFromEncoder(audioEnc, outChannels, outLayout, outRate)

// 5. In the packet loop:
frames, _ := audioDec.Decode(avPkt)
for _, frame := range frames {
    outFrame, _ := audioResample.Convert(frame)
    frame.Free()
    encPkts, _ := audioFifo.Write(outFrame)  // NOT audioEnc.Encode()
    outFrame.Free()
    for _, pkt := range encPkts {
        muxer.WriteAudioPacket(pkt)
        pkt.Free()
    }
}
```

### Video Decoder

```go
// From CodecParameters (preferred — copies all codec config)
videoDec, err := decode.NewVideoDecoderFromParams(dm.VideoCodecParameters(), decode.DecodeOpts{
    HWAccel: "videotoolbox",  // or "vaapi", "nvenc", "none"
})
```

## Tested Input Codecs

All auto-detected, all working:

| Type | Codecs |
|------|--------|
| Video | H.264, HEVC, MPEG-2, MPEG-4 |
| Audio | AAC, AAC-LATM, AC3, EAC3, DTS, TrueHD, FLAC, MP2, MP3, Vorbis, Opus, PCM S16LE |

Any channel count (mono through 7.1), any sample rate (44.1kHz, 48kHz, etc.), any sample format (s16, s32p, fltp, etc.) — the resampler handles all conversions automatically.
