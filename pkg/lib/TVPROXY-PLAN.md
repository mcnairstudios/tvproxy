# tvproxy AV Pipeline Requirements

Issues from the tvproxy reviewer agent. Numbered for coordination.

## BLOCKING PLAYBACK (highest priority)

### REQ-001: MSE Copy Pipeline (no decode/encode)

When needsTranscode=false and no bitrate/resolution override, MSE must pass compressed packets straight to the fMP4 muxer. Currently runGoMSE always creates NewMSETranscodePipeline which decodes+encodes. On Mac this causes "Generic error in an external library" for 4K HEVC.

Three MSE modes needed:
1. Copy — outCodec == srcCodec AND Bitrate == 0 AND OutputHeight == 0: packets → fMP4 muxer. Zero CPU.
2. Transcode (different codec) — e.g. MPEG-2 → H.264: decode → encode → fMP4.
3. Transcode (same codec, different params) — e.g. H.265 4K → H.265 1080p: decode → scale → encode → fMP4.

Decision: forceDecode = needsTranscode || Bitrate > 0 || (OutputHeight > 0 && OutputHeight < sourceHeight)

Audio always needs transcode to AAC stereo for browser MSE (unless source is already AAC stereo — then passthrough).

### REQ-011: Chrome MSE Segment Parsing Failed

Chrome rejects fMP4 segments: CHUNK_DEMUXER_ERROR_APPEND_FAILED. Init segments written but Chrome can't parse them. Likely: wrong codec string (must be hev1 not hvc1 for HEVC), missing extradata in moov, or timestamps not starting from 0. Verify by dumping init_video.mp4 with mp4dump.

UPDATE: HEVC copy mode works (1917 plays, DTS audio → AAC transcode). H.264 copy mode fails.

Confirmed on tvproxy side: copy mode correctly selected (transcode=false, out_video=h264, src_video=h264). WG routing working. Init segments written. Media segments produced and served (200 responses). Chrome rejects every segment.

The FragmentedMuxer produces valid fMP4 for HEVC but not for H.264. Likely causes:
- Wrong sample entry type in init segment (needs avc1, not avc3)
- Missing or malformed avcC box (SPS/PPS must be in moov, not inline)
- H.264 Annex B start codes not converted to length-prefixed NALUs in mdat
- Audio init segment also failing (mseAudioSb ERROR null) — audio source is aac_latm, going through decode→AAC encode path (passthrough correctly rejected). Something in the LATM decode or AAC encode produces an init segment Chrome can't use.

Test: dump init_video.mp4 and init_audio.mp4 with mp4dump to verify box structure. Compare with the working HEVC init segments from 1917.

### REQ-023: H.264 MSE Copy Mode — Audio Segments Never Produced, Video Segments Huge

BLOCKING PLAYBACK for H.264 content in copy mode.

**Reproduction:**
- Content: "Slow Horses S01E01" from tvproxy-streams (http://192.168.1.149:8090/stream/831b51eb1f0bb302)
- Source: H.264 1920x1080, PCM S16LE 2ch audio
- Profile: Browser, MSE, Match Source (copy mode)
- Pipeline: MSE copy mode — no decode/encode
- HW accel: videotoolbox (not used in copy mode)
- Audio: pcm_s16le 2ch → AAC 2ch 48kHz (decode→resample→encode)

**Observed:**
- Init segments written: video 787 bytes, audio 709 bytes
- Video segments produced but VERY slowly: one every ~15 seconds, each ~30MB
- ZERO audio segments produced — all audio requests 404
- Browser spins because no audio = Chrome MSE won't start playback
- Fragment duration appears to be ~15 seconds (should be ~2 seconds)

**Video segment sizes (too large):**
```
video_0001.m4s  10,869,812 bytes (~10MB)
video_0002.m4s  30,607,718 bytes (~30MB)
video_0003.m4s  30,101,434 bytes (~30MB)
video_0004.m4s  30,416,919 bytes (~30MB)
video_0005.m4s  30,693,436 bytes (~30MB)
```

For 1080p H.264 at ~20Mbps, 30MB = ~12 seconds per segment. Way too long. Should be ~2 seconds (~5MB).

**Comparison with working HEVC (1917):**
- 1917 HEVC copy mode: segments produced every ~3-4 seconds, both video and audio
- Slow Horses H.264 copy mode: segments every ~15 seconds, audio never flushes

**Two issues:**
1. Video fragment flush threshold is too high for H.264. The keyframe interval in this H.264 source may be 10-15 seconds (open GOP). The muxer flushes on keyframes, so segments are 10-15 seconds long. Consider adding a max duration flush (e.g., 4 seconds) even without a keyframe.

2. Audio fragments never flush. The audio is PCM S16LE → AAC transcode. The muxer's audio track may not be getting any data, or the AAC encoder isn't producing packets, or the audio fragment flush logic isn't triggering. pcm_s16le is an unusual source codec — verify it's in the codec map and the decoder handles it correctly.

### REQ-025: Empty codec_string in probe.pb — Chrome Can't Create SourceBuffer

BLOCKING PLAYBACK for SAT>IP H.264 live and possibly all H.264 streams.

**Reproduction:**
- Channel: "5 HD" (SAT>IP DVB-T2)
- Source URL: rtsp://192.168.1.149/?freq=545.833&msys=dvbt2&mtype=256qam&pids=0,6400,6401,6402,6406,6405&bw=8&plp=0
- Source: H.264 1920x1080, AAC-LATM 2ch
- Profile: Browser, MSE, Match Source (copy mode)
- Pipeline: MSE copy mode, H.264, audio AAC-LATM → AAC transcode
- HW accel: videotoolbox (not used in copy mode)

**Observed:**
- Segments flowing correctly: 35 video segments, 13 audio segments in 30 seconds
- All segment requests return 200
- Init segments: video 787 bytes, audio 709 bytes
- No errors in logs
- Chrome shows nothing — no video, no audio, just spinner

**Root cause:**
probe.pb shows `codec_string: ""` (empty). The frontend needs this to call `addSourceBuffer('video/mp4; codecs="avc1.640028,mp4a.40.2"')`. Without a valid codec string, Chrome can't decode the segments.

Expected codec strings:
- H.264: `avc1.PPCCLL` (e.g., `avc1.640028` = High profile, level 4.0) — computed from SPS bytes in avcC box
- HEVC: `hev1.P.CC.TL` (e.g., `hev1.1.6.L150`) — computed from VPS/SPS in hvcC box  
- AAC: `mp4a.40.2` (AAC-LC)

The `FragmentedMuxer.VideoCodecString()` should extract this from the init segment's sample entry box. `EnrichProbeFile()` writes it to probe.pb. Currently returning empty string.

Note: 1917 (HEVC copy mode, DTS→AAC) also had empty codec_string but somehow played. May have been Chrome being lenient with HEVC, or the frontend fell back to a generic type.

### REQ-026: SAT>IP Live Startup Too Slow — 19 Seconds

Session created to pipeline started takes 19 seconds for SAT>IP (rtsp://192.168.1.149). The old GStreamer path was 2-3 seconds.

The demuxer's `Reconnect()` sets `analyzeduration=10000000` (10s) and `probesize=10000000` (10MB) which forces libavformat to buffer 10 seconds before starting. This is redundant — the probe already detected all streams in ~150ms.

The INTEGRATION.md specifies single-open: the demuxer and probe share the same FormatContext. The demuxer should use the probe's stream info and start reading packets immediately. `find_stream_info` should NOT be called again on reconnect if the stream info is already known from the initial open.

For initial open, the probe takes ~150ms. The demuxer should be ready to produce packets within 200-300ms of session creation, not 19 seconds.

**Note:** pcm_s16le has no compression — each "packet" is raw samples. The decode step should be trivial (just reformat to fltp). Verify `conv.CodecIDFromString("pcm_s16le")` returns the correct codec ID.

### REQ-024: Audio Codec-to-Encoder Name Mapping

**Audio encoder:**
`NewAudioEncoder` does `FindEncoderByName(codecName)` directly. tvproxy passes codec names like `"opus"`, `"mp3"`, `"flac"`. But ffmpeg encoder names are different:
- `"opus"` → encoder is `"libopus"`
- `"mp3"` → encoder is `"libmp3lame"`
- `"vorbis"` → encoder is `"libvorbis"`
- `"aac"`, `"ac3"`, `"eac3"`, `"flac"`, `"mp2"` → encoder name matches codec name

Either add a lookup table in `NewAudioEncoder`, or have the caller pass the encoder name (but that defeats the purpose of a clean API). Recommended: add a `audioEncoderName` map similar to `encoderTable`.

### REQ-022: Accept OutputAudioCodec in Pipeline Opts

BLOCKING PLAYBACK for H.264+AAC_LATM IPTV streams.

manager.go now passes `OutputAudioCodec` (e.g. "aac") to all four pipeline opts: MSECopyOpts, MSETranscodeOpts, StreamCopyOpts, FullTranscodeOpts. DONE by tvproxy reviewer.

AV package needs to:
1. Add `OutputAudioCodec string` field to MSECopyOpts, MSETranscodeOpts, StreamCopyOpts, FullTranscodeOpts.
2. When `OutputAudioCodec` is set (e.g. "aac"), always decode source audio and encode to that codec. No passthrough.
3. When `OutputAudioCodec` is "copy" or empty, passthrough is allowed.
4. Remove the hardcoded `audioPassthrough = audioTrack.Codec == "aac" && audioTrack.Channels <= 2` logic — let OutputAudioCodec drive the decision.

Test with H.264+AAC_LATM (IPTV) and HEVC+DTS (1917 VOD).

### REQ-012: AAC Encoder Frame Size

aac: frame_size (1024) was not respected for a non-last frame. Resampler output doesn't align to AAC frame boundaries. Resampler should output exactly 1024-sample frames, or encoder needs to buffer internally.

### REQ-013: mp4 Muxer "codec frame size is not set" — FIXED

mp4 track 0: codec frame size is not set. Audio stream codec parameters need frame_size=1024 for AAC.

### REQ-014: MSE Copy Mode — WriteVideoPacket "Invalid argument"

MSE copy pipeline is active (confirmed: "MSE copy mode — no decode/encode" in logs). Init segments written (video 1470 bytes, audio 709 bytes). But WriteVideoPacket fails with "Invalid argument" on the first compressed packet. No media segments produced.

The FragmentedMuxer.WriteVideoPacket receives raw compressed HEVC packets in copy mode. The AVPacket needs:
- Correct stream_index (matching the video stream added to the muxer)
- PTS/DTS in the muxer's time_base (rescaled from demuxer time_base)
- Correct flags (keyframe flag set for IDR frames)
- Duration set (or 0)

The copy pipeline's PushVideo creates the AVPacket — verify it sets all these fields correctly before calling WriteVideoPacket.

### REQ-015: MSE Copy Mode — No Media Segments Written

MSE copy mode activates correctly ("MSE copy mode — no decode/encode" logged). Init segments written (video 1470 bytes, audio 709 bytes). But NO media segments ever appear — no video_0001.m4s, no audio_0001.m4s. Pipeline runs silently with no errors logged. Segments directory stays at just init files for minutes.

Either:
1. Demux loop isn't pushing packets to the copy pipeline (demux error swallowed)
2. FragmentedMuxer receives packets but never flushes a fragment (keyframe detection not working in copy mode, or fragment duration threshold not hit)
3. WriteVideoPacket returns an error that's silently discarded

Add debug logging to MSECopyPipeline.PushVideo to confirm packets are arriving and track why flushFragment isn't called.

### REQ-016: Seek Causes SIGSEGV in av_seek_frame

Playback works for 1917 (HEVC copy mode). But seeking causes SIGSEGV:
- `av_seek_frame` called on the FormatContext
- "Seek to desired resync point failed. Seeking to earliest point available instead."
- Then SIGSEGV in CGO — likely use-after-free on the FormatContext

The seek flow: MSESeek handler → VODService.SeekSession → Manager.RestartWithSeek → Session.Seek → DemuxSession.SeekTo → Demuxer.SeekTo → av_seek_frame.

Possible causes:
- FormatContext freed by another goroutine while seek is in progress
- Seek called concurrently with ReadPacket on the same FormatContext (not thread-safe)
- Muxer/pipeline not flushed before seeking — stale references

After seek, the pipeline also needs to write new init segments and reset segment numbering. The watcher needs a generation bump so the frontend restarts its fetch loop.

UPDATE: No longer crashing on seek (SIGSEGV fixed). But seek is broken functionally:
- No generation reset — watcher keeps old generation, frontend doesn't restart fetch
- Segment numbers continue from pre-seek values (video_0021...) instead of restarting at 1
- PTS/DTS not rebased after seek: "Packet duration: -3068 is out of range", "pts has no value"
- Muxer produces corrupt segments after seek point

Seek must: (1) stop demux loop, (2) flush+reset muxer (new init segments, seq 1), (3) reset watcher (generation bump → frontend gets 410 → restarts), (4) rebase PTS/DTS to 0, (5) restart demux loop.

UPDATE: Seek now restarts the pipeline. Init segments are written (confirmed on disk at correct timestamp). But the watcher returns 503 for init segments after seek — it doesn't pick up the new files. Either fsnotify misses the writes (files written before watcher recreated) or the watcher's atomic pointers are reset and never re-populated from the existing files on disk.

The watcher needs to either: (a) scan the segments directory for existing files on creation/reset, or (b) the init segments must be written AFTER the watcher is recreated (not before).

UPDATE 3: In-place seek wired via RequestSeek + SetOnSeek. Seek fires, muxer.Reset() + watcher.Reset() called. Init segments produced after seek. Data flows instantly.

But post-seek segments have WRONG PTS. Verified with ffprobe:
- Pre-seek segments: start_time=0.000000 (correct)
- Post-seek segments after seeking to 1374s: start_time=3784.280000 (wrong — should be ~1374)

muxer.Reset() does NOT reset its PTS/DTS tracking. The muxer continues accumulating from where it was before the seek. After a backward seek (1374s when movie was at ~3784s), the new packets have PTS at 1374 but the muxer maps them to its internal timeline at 3784+.

Fix: muxer.Reset() must fully reinitialise PTS/DTS state. The first post-seek packet should establish a new baseline. The output PTS should match the input PTS (movie timeline). If the seek is to 1374s, the segments should have start_time=1374.

Also: "Packet duration: -90" warnings on EVERY audio packet (even pre-seek). This is a separate issue — the audio PTS conversion has a rounding error producing slightly negative durations. These warnings may cause audio segment starvation which prevents Chrome from starting playback.

UPDATE 4: Verified with ffprobe — post-seek segments have start_time=0.000000 (not movie time). The demuxer resets basePTS to -1 on seek (line 491 of demux.go), so the first post-seek packet becomes PTS 0. This contradicts TVPROXY-CHANGES.md which says "PTS is movie time. No rebasing."

The frontend creates a fresh MediaSource and sets currentTime = buffered.start(0) which is 0. MediaSource.duration is set to the total movie length. Chrome should play from 0 but the seekbar would show position 0 not the seek position.

For correct seeking: either (a) demuxer should NOT rebase PTS after seek (keep movie time), or (b) the frontend should set currentTime to 0 after seek since data starts at 0, and use timestampOffset on the SourceBuffer to map PTS 0 to the seek position.

Option (a) is simpler — remove the basePTS reset in the demuxer's seek handler. The muxer's new trackMuxer has fresh DTS tracking and will accept the movie-time PTS.

UPDATE 5: PTS is now movie time (basePTS fix confirmed working). Init segments load after seek. Segments flow. But Chrome won't play because video and audio PTS are misaligned:
- Video start_time: 25.209s
- Audio start_time: 33.109s (8 seconds ahead)
- 4 video segments, 18 audio segments

Chrome MSE needs both tracks buffered at the SAME timeline position. With an 8-second gap, they never overlap enough to start playback.

Fix: after seek, the demuxer should align both tracks. Drop audio packets until the first video keyframe arrives, then start both tracks from the same PTS. Or hold audio in a buffer until video catches up.

UPDATE 6: Confirmed massive A/V desync after seek. Probed latest segments:
- Video PTS: 2522s
- Audio PTS: 2446s
- Gap: 76 seconds — audio is behind video

Also: seek position is inaccurate. User requested ~35 minutes, backend received 2701s (45 minutes), playback started at ~4 minutes. The seek position calculation in the frontend may be wrong, or the demuxer seeked to the wrong keyframe.

Both issues need the demuxer to properly align tracks after seek and seek to the correct position.

UPDATE 2: Seek now works mechanically — pipeline restarts, init segments written, segments flow. But no picture after seek. All segments return 200, no Chrome errors. Audio fetches much faster than video.

The likely cause: PTS after seek starts at the seek position (e.g., 3125s) instead of being rebased to 0. Chrome MSE SourceBuffer had data at 0-535s, then gets new data at 3125s+. It can't bridge the gap. The demux loop must rebase PTS to 0 after seek — subtract the seek offset from all packet timestamps.

### REQ-017: Deinterlace Filter — "Resource temporarily unavailable" on SAT>IP

Live SAT>IP RTSP playback fails immediately: `demuxloop: push video: deinterlace: filter: getting frame from buffersink: Resource temporarily unavailable`

SAT>IP DVB-T content is typically interlaced (576i/1080i). The deinterlacer is created when `info.Video.Interlaced=true` or `opts.Deinterlace=true`. The avfilter buffersink returns EAGAIN ("Resource temporarily unavailable") — meaning it needs more input frames before producing output.

The deinterlacer (yadif/bwdif) buffers frames: it needs the current + next field to produce a deinterlaced frame. On the first frame, it returns EAGAIN because it doesn't have enough context yet. The caller must handle EAGAIN by continuing to feed frames, not treating it as a fatal error.

Fix: In the deinterlace Process() function, if buffersink returns EAGAIN, return nil (no output frame yet) instead of an error. The caller should skip that iteration and continue the demux loop. Deinterlaced frames will start appearing after 1-2 input frames.

Note: HDHomeRun live playback works fine (H.264, non-interlaced). SAT>IP is the only failing live path — it's the interlaced content that triggers the deinterlacer.

### REQ-018: Audio/Video Segment Rate Imbalance

1917 (4K HEVC + DTS 6ch) produces video segments at ~3x the rate of audio segments. Video: 10MB segments every ~3s. Audio: 33KB segments every ~10s. Chrome MSE won't start playback until both buffers have enough data — the audio starves the player.

The DTS→AAC transcode produces audio fragments too infrequently. Either:
- The fragment flush threshold for audio is too high (should flush every ~2s like video)
- DTS decode drops frames and the AAC encoder doesn't produce output fast enough
- The audio muxer only flushes when it has a full fragment, and the threshold is based on time not samples

Initial playback starts immediately. The audio/video imbalance may be a throughput issue — 4K HEVC at 50-60Mbps from a remote server over WireGuard produces 10MB video segments. Network bandwidth, not muxer rate, may be the bottleneck. Test with local content before investigating further.

### REQ-021: libavformat Can't Open Stream Through WG Proxy

WG proxy routing works (URL rewritten correctly). But libavformat fails to open the proxied URL: "Invalid data found when processing input". The proxy returns raw MPEG-TS bytes but libavformat's HTTP client may need specific response headers (Content-Type, no chunked encoding) for format detection.

The proxy at `127.0.0.1:{port}/?url={encoded_url}` works with curl but not with libavformat's `avformat_open_input`. May need to set `Content-Type: video/MP2T` or similar on the proxy response, or increase libavformat's `probesize` for proxied streams.

### REQ-020: Video Encoder HW→SW Fallback

VideoToolbox H.265 encoding fails on Mac with "Generic error in an external library" for some inputs (e.g., live IPTV H.264→H.265 transcode). The encoder should fall back to software (libx265/x265) when HW encoding fails, same as the decode path falls back to SW.

Currently avencode.NewVideoEncoder tries the HW encoder and if it fails at runtime (not at init), the whole pipeline fails. It should catch the first encode error and rebuild the encoder chain with SW fallback.

### REQ-019: B-frame PTS Ordering in Copy Mode

Chrome warns: "presentation time of most recently processed random access point (0.876s) is later than the presentation time of a non-keyframe (0.792s) that depends on it."

In HEVC copy mode, B-frames have decode order ≠ presentation order. The keyframe PTS can be higher than dependent non-keyframes. Chrome MSE doesn't handle this well — buffered range reporting becomes imprecise.

This may require sorting packets by PTS before writing to the fMP4 muxer in copy mode, or setting composition time offsets (ctts) correctly in the moof.

## FIXED (verify still working)

### REQ-002: aac_latm codec map — FIXED
### REQ-003: "default" output codec resolved — FIXED in manager.go
### REQ-004: DTS decode failures — latched, video continues
### REQ-005: Demuxer analyzeduration/probesize — FIXED
### REQ-006: Memory .Free() calls — FIXED
### REQ-007: FindStreamInfo SIGSEGV — FIXED
### REQ-008: NAL unit packet conversion — FIXED
### REQ-009: Video decoder missing codec params — FIXED
### REQ-010: probe.pb container name vs codec — FIXED
