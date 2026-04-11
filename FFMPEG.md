# FFmpeg Reference (for potential re-integration)

This documents all ffmpeg knowledge accumulated during tvproxy development.
GStreamer is the primary transcoding engine. This file exists as reference
if ffmpeg needs to be re-added.

## Optimal DVB-T/HDHR Settings

```
-err_detect ignore_err
-fflags +igndts+genpts+discardcorrupt+nobuffer
-f mpegts
-probesize 200000
-analyzeduration 0
-i pipe:0 (with TS header injection) or http://device:5004/auto/v101
-map 0:v:0? -map 0:a:0?
-c:v copy -c:a aac -b:a 192k
-f mpegts
```

### Startup Timing (HDHR DVB-T, tested 2026-04-11)
- Default (no optimization): ~4.75s
- With TS header injection (shell pipe): ~1.65s
- With TS header injection (Go pipe): ~1.71s
- GStreamer transcode (VideoToolbox): ~2.84s
- ffmpeg transcode (VideoToolbox): ~4.46s

### TS Header Injection
The SPS/PPS arrives every ~1s in DVB-T streams. By caching the PAT+PMT+SPS
TS packets (~564 bytes) from a previous connection and prepending them to
the stdin pipe, ffmpeg gets codec parameters immediately without waiting.

Each channel has unique PIDs — headers must match the channel being played.

## Hardware Acceleration

### VAAPI (Intel A380)
```
-hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi
-c:v h264_vaapi / hevc_vaapi / av1_vaapi
```
- Benchmarks (4K): H264 106fps, HEVC 111fps, AV1 91fps
- 10-bit H264: needs `scale_vaapi=format=nv12`
- Hardware deinterlace: `deinterlace_vaapi` (not `yadif`)
- ffmpeg 8.x removed `-vaapi_device`, use `-init_hw_device vaapi=va:/dev/dri/renderD128`

### QSV
```
-hwaccel qsv -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format qsv
-c:v h264_qsv / hevc_qsv
```

### VideoToolbox (Mac)
```
-c:v h264_videotoolbox
```

## Audio Codec Handling

### AAC-LATM (DVB-T HD audio)
- Cannot be muxed into MP4 container directly
- Must transcode to AAC: `-c:a aac -ac 2 -b:a 192k`
- GStreamer equivalent: `avdec_aac_latm ! audioconvert ! faac`

### MP2 (DVB-T SD audio)
- Needs transcoding for browser/HLS: `-c:a aac`
- GStreamer: `mpg123audiodec ! audioconvert ! faac`

## Dual Output (HLS + Recording)
```
ffmpeg ... -i input \
  -c:v copy -c:a aac ... -f hls -hls_time 6 -hls_segment_type fmp4 ... playlist.m3u8 \
  -c:v copy -c:a copy -bsf:a aac_adtstoasc -f mp4 -movflags frag_keyframe+empty_moov recording.mp4
```

Note: mp4 output with aac_latm audio needs `-c:a aac`, not `-c:a copy`.

## IPTV Source Settings
```
-analyzeduration 500000 -probesize 500000
-err_detect ignore_err -fflags +genpts+discardcorrupt
-f mpegts
```

## SAT>IP (RTSP) Settings
```
-rtsp_transport tcp
-analyzeduration 3000000 -probesize 10000000 -max_delay 500000
-err_detect ignore_err -fflags +genpts+discardcorrupt
-fps_mode cfr
```
Deinterlace needed for SD DVB-S content.

## Probe Timing
- ffprobe on local file: ~50ms
- ffprobe on live HDHR stream: ~8-10s (SPS/PPS wait)
- gst-discoverer on local file: ~200ms
- gst-discoverer on live stream: ~10s

## Container Compatibility
- AV1 + MPEG-TS: doesn't work (no official stream type)
- WebM requires Opus audio, not AAC
- HLS fMP4 segments need h264parse output

## MapEncoderHW (codec + hwaccel → encoder name)
- h264 + vaapi → h264_vaapi
- h265 + vaapi → hevc_vaapi
- av1 + vaapi → av1_vaapi
- h264 + qsv → h264_qsv
- h265 + qsv → hevc_qsv
- h264 + nvenc → h264_nvenc
- h264 + videotoolbox → h264_videotoolbox

## Key Files (pre-GStreamer)
- pkg/ffmpeg/compose.go — stream profile arg composition
- pkg/ffmpeg/build.go — hwaccel init flags
- pkg/ffmpeg/args.go — ShellSplit, InjectReconnect, MapEncoderHW
- pkg/ffmpeg/probe.go — ProbeResult struct, ffprobe wrapper
- pkg/session/manager.go — buildDualOutputArgs, buildArgs
- pkg/hls/session.go — HLS session with ffmpeg
