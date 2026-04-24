# Fixes & Ideas (Not on Critical Path)

## Jellyfin (NEW — April 2026)

### Playback (CRITICAL)
- PlaybackInfo TranscodingUrl missing params: VideoBitrate, AudioBitrate, MaxFramerate, BreakOnNonKeyFrames, TranscodingMaxAudioChannels, RequireAvc, EnableAudioVbrEncoding, Tag, codec profile params
- TranscodingUrl video ID should use dashed format in path
- MediaSource needs VideoType: "VideoFile", Bitrate, Size, Formats:[], MediaAttachments:[], RequiredHttpHeaders:{}, SupportsProbing, ReadAtNativeFramerate, HasSegments, ETag
- HLS master/media playlist handlers need to work with AV pipeline (current HLS manager is old GStreamer-based)
- PlaybackInfo should parse client DeviceProfile from POST body for codec negotiation
- Client sends full device capabilities — we should use them to determine direct play vs transcode

### TV Series
- TV series browsing not wired into Jellyfin client (reference flow in /tmp/jfproxy.log)
- Series → Seasons → Episodes navigation needs implementing
- Episode detail view needs series context (SeriesName, SeasonId, IndexNumber, ParentIndexNumber)

### HLS Delivery Mode for libavformat
- Add HLS as a third delivery option alongside Stream and MSE
- AV pipeline outputs TS segments to disk (like old GStreamer HLS path)
- Serve via existing pkg/hls handlers (master.m3u8, media.m3u8, segments)
- Jellyfin client requires HLS — TranscodingProfiles specify Protocol: "hls", Container: "ts"
- Wire into client stream profiles as delivery: "hls"
- This unblocks Jellyfin playback and any other client that needs HLS

### Probe Data → Duration + Codec
- lib/av probe should capture duration from avformat and save it with probe data
- Propagate duration back into the stream store (VODDuration field)
- VODVCodec / VODACodec should be populated from probe data
- Remove TMDB runtime hack once probe duration is available
- This feeds into accurate RunTimeTicks for Jellyfin and correct MediaStream codec info

### Token Auth
- Auto-register unknown tokens to first user is temporary hack
- Need proper multi-user token validation, expiry, revocation

### Response Format
- All ID handling needs normaliseID (stripDashes) at entry point — currently ad-hoc
- enrichMovieDetail was stripped too aggressively — add back fields one at a time with testing
- Season IDs ("cccc0000" prefix) need validation with dashed format from client

### Missing Features
- Live TV channels not accessible from Jellyfin client
- No playback progress reporting (Sessions/Playing)
- No watch history / resume
- No search
- WebSocket sends no events

### Docker / Build
- sdk_models.go reference from Opus agent needs copying from worktree branch
- HLS code in playback.go references old HLS manager — needs updating for AV pipeline

## How to Test

```bash
# Build (requires libavformat/libavcodec/libavutil/libavfilter/libswscale/libswresample dev libs)
CGO_ENABLED=1 go build -o ./tvproxy ./cmd/tvproxy/

# Run
TVPROXY_USER_AGENT="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36" \
TVPROXY_RECORD_DIR=/tmp/recordings \
TVPROXY_VOD_OUTPUT_DIR=/tmp/recordings \
TVPROXY_BASE_URL=http://192.168.0.111 \
./tvproxy
```

## High Priority

### 1. A/V Sync — Audio Timescale Must Come From Probe
**RESOLVED:** Root cause was hardcoded `timescale=48000` in TrackStore for all audio, regardless of source sample rate. Source at 44100Hz produced 9% frame duration error → ~6s drift per minute. Combined with GStreamer burst delivery (~7s head start), total offset was 12-14 seconds.

**Fix applied:** Probe captures `sample_rate` per audio track. Session manager calls `SetAudioRate()` before init segment is built. No more hardcoded 48000 anywhere.

**Remaining risks:**
- If probe fails to return sample_rate, timescale defaults to 0 (will break). Need fallback detection from first GStreamer buffer.
- Audio copy mode preserves source sample rate. If source changes rate mid-stream, we won't detect it.

### 2. tfdt (BaseMediaDecodeTime) Continuity Is Sole Source of Truth
The browser MSE SourceBuffer in `segments` mode uses `tfdt` from each moof box to place media on the timeline. Go TrackStore is the sole producer of these values. The rule is absolute:
- `Audio Segment[N+1].tfdt = Audio Segment[N].tfdt + Audio Segment[N].Duration`
- `Video Segment[N+1].tfdt = Video Segment[N].tfdt + Video Segment[N].Duration`

If these are off by even 1 tick, the browser detects a gap or overlap → `waiting` event or SourceBuffer error. Currently, `decodeTime` is accumulated by summing `sample.Dur` values (trackstore.go:481, 530). This is correct **if and only if** `resolveDuration()` returns the exact right value for every frame.

**Risks:**
- `resolveDuration()` uses PTS diff for video and buffer duration for audio. PTS diff can jitter — a single outlier propagates as a tfdt gap forever.
- The correction at flushSegment (lines 250-269) adjusts the last sample's duration to align with PTS, but only if the gap is < 3x average. Large bursts (e.g. 7s audio head start) may exceed this threshold.
- On seek/reset, `decodeTime` resets to 0 but sharedBaseNs also resets. If the pipeline doesn't produce a clean first PTS after seek, the new timeline starts with wrong offset.
- Audio duration fallback is hardcoded 1024 ticks (trackstore.go:381). At 44100Hz that's 23.2ms per frame, correct for AAC-LC. But at 48000Hz that's 21.3ms — wrong for 1024-sample AAC frames at that rate. The fallback should be `1024` only when timescale matches the expected frame size.

### 3. GStreamer Burst Delivery — Audio Arrives ~7s Ahead of Video
Both appsinks use `sync=false`. Audio (copy or fast transcode) arrives nearly instantly. Video (H265 encode) has ~7s latency through the encoder. This means audio TrackStore accumulates ~7s of content before video produces its first sample.

**Impact on tfdt:** Audio tfdt races ahead. The sharedBaseNs mechanism (trackstore.go:354-367) sets the shared base from whichever track produces first PTS — always audio. Video's first PTS will be ~7s later, so video's `ptsToTicks` returns ~7s worth of ticks, which is correct. But audio's `decodeTime` is already at ~7s of accumulated ticks. The partner flush gate (`PushAudioFrame` returns early until video init is ready, line 498) prevents segment emission, but frames still accumulate in pendingSamples.

**Risks:**
- If `sharedBaseNs` is set by audio's first PTS and video's first PTS is 7s later, the video segment starts at tfdt=0 but the audio segment should also start at tfdt=0. The partner gate handles this (audio waits for video init). But after video init, all 7s of buffered audio flushes at once — one giant segment that covers 0-7s. Browser handles this as a single large segment, which should be fine. **Verify this works in practice.**
- If any audio frames are dropped during the burst (GStreamer queue overflow), there's a permanent tfdt gap in the audio timeline.

### 4. Audio MIME Type Hardcoded in Frontend
**RESOLVED:** `web/dist/app.js` was hardcoding `audio/mp4; codecs="mp4a.40.2"`. Now reads `audio_codec` from the MSE debug endpoint (`GetAudioCodecString()` on TrackStore). Falls back to `mp4a.40.2` if endpoint doesn't return it.

### 5. Audio Channel Count Hardcoded to Stereo
**RESOLVED:** `buildAudioChain` and `buildAudioChainDecoded` now accept a `channels` parameter. `audioChannels(opts)` helper returns 2 for MSE/browser delivery (stereo requirement), source channel count for file output. All 5 call sites updated. String pipeline path also updated.

**Remaining:** `audio_channels` field not yet added to client stream profiles. Currently derived from probe data + delivery mode only.

### 6. Initial A/V Offset (start_time) Not Applied
Probe captures `VideoInfo.StartTime` and `AudioTrack.StartTime` per stream. These represent the PTS of the first frame in the source. If video starts at 0.5s and audio starts at 0.0s, there's a 500ms inherent offset in the source.

**Current state:** `start_time` is stored in ProbeResult but never used in the pipeline or TrackStore. The shared base PTS mechanism uses the first GStreamer PTS from each track, but GStreamer may adjust these relative to the pipeline clock, discarding the original offset.

**Impact:** Sources with A/V offset baked into the container (common in MPEG-TS recordings, broadcast captures) will play back with wrong lip sync even if everything else is perfect.

**Fix:** Pass source `start_time` to the pipeline or to the TrackStore. Use the delta `(video_start_time - audio_start_time)` to offset the sharedBaseNs for one track.

### 7. Probe Failure Leaves Audio Timescale at Zero
If probe fails or returns no audio tracks, `SetAudioRate()` is never called. `timescale` stays at 0 (set in `NewTrackStore`). Division by zero in `GetTimingDebug()` (line 156: `float64(ts.decodeTime) / float64(ts.timescale)`). Duration calculation in `resolveDuration()` divides by 1e9 then multiplies by timescale — timescale=0 means all durations are 0, all tfdts stay at 0, browser shows nothing.

**Fix:** Detect timescale=0 when first audio frame arrives. Extract sample rate from the AAC ADTS header (bytes 2-3 encode sample rate index) or from GStreamer buffer caps. Set timescale before building init segment.

### 8. Tuner Contention
SAT>IP copy test failed with 0 bytes when tuner was locked by prior session. HDHR errored with "Internal data stream error" on tuner conflict.

**Fix:** Session manager checks tuner availability before creating new pipeline.
- `pipeline.SetState(StateNull)` must complete synchronously before removing session
- Wait for RTSP TEARDOWN before starting new session on same tuner
- Per-source connection limits in M3U account settings

### 9. Auto-Recovery for Live Stream Pipeline Drops
When live pipeline EOS's (DVB signal drop) or errors, should auto-restart.

**Fix:**
- Keep same session/consumers, rebuild GStreamer pipeline
- Retry count + backoff (max 3 retries, 2s between)
- Frontend MSE handles reconnect via generation counter (already implemented)

### 10. RTSP Copy Mode Produces 0 Bytes
RTSP source with h264parse → mp4mux produces 0 bytes. Transcode works (re-timestamps). HTTP source copy works.

**Root cause:** RTSP RTP timestamps don't align with mp4mux expectations.

**Options:**
1. RTSP copy → use mpegtsmux instead of mp4mux
2. RTSP copy → always transcode (defeats purpose)
3. RTSP copy → use plugin path via gst-launch subprocess

**Impact:** SAT>IP copy mode only. Browser playback (always transcode) NOT affected.

### 11. ensureProbe Doesn't Use WireGuard
`ensureProbe()` uses libavformat directly (no custom HTTP client). WireGuard-routed sources fail to probe.

**Fix:** Use the probe scheduler's method instead of direct avprobe. The scheduler already handles WG routing.

### 12. ForceSeedClientDefaults Wipes User Customizations
`ForceSeedClientDefaults` (called on every startup in main.go) clears all clients and re-seeds. Any user edits to client profiles are lost on restart. `SeedClientDefaults` (skip-if-non-empty) is only used internally now.

**Fix:** main.go should call `SeedClientDefaults` (non-force). Only `HardReset` should call `ForceSeedClientDefaults`.

### 13. Hardcoded 16:9 Aspect Ratio for Output Scaling
`builder.go` uses `pixel-aspect-ratio=1/1` in scale caps which respects display aspect ratio. But `bitrate()` still calculates width as `OutputHeight * 16 / 9` for bitrate selection. Non-16:9 content gets wrong bitrate estimate.

**Fix:** Use `SourceWidth` and `SourceHeight` from probe for bitrate calculation when available.

## Medium Priority

### 14. No Adaptive Bitrate (ABR) — Single Quality Only
MSE path produces one quality level. Clients on slow connections get rebuffering. Clients on fast connections get unnecessarily low quality if bitrate is conservative.

**Impact:** Poor experience on variable networks (mobile, WiFi). Every professional streaming service uses ABR (multiple renditions, manifest-based switching).

**Fix:** Generate multiple renditions (e.g. 360p/720p/1080p) from single GStreamer pipeline using tee element. Serve HLS manifest with multiple quality levels. hls.js handles ABR switching automatically.

### 15. Segment Duration Mismatch Between Video and Audio
Video segments flush on keyframe + 2s wall-clock boundary (`PushVideoFrame` line 489). Audio segments flush when video's partner calls `FlushNow()`. The audio segment duration depends on how many audio frames accumulated since last flush — not guaranteed to be exactly 2s.

**Impact:** HLS playlists declare segment duration. If video says 2.0s but audio is 2.1s, some players may have sync drift over long playback. MSE mode is less affected because browser uses tfdt, not declared duration.

**Fix:** Ensure audio segment boundaries align with video segment boundaries in wall-clock time. Or use `EXT-X-INDEPENDENT-SEGMENTS` in HLS and let the player handle it.

### 16. Migrate proxy.go to Native Build()
`service/proxy.go` still uses `gst-launch-1.0` subprocess via `BuildPipeline()`. Need `Build()` variant that outputs to `fdsink`/`appsink` instead of `filesink`.

### 17. Migrate Jellyfin HLS to Native Build()
`hls/session.go` and `jellyfin/playback.go` use old `BuildPipeline()` for string pipelines. HLS output needs `hlssink2`/`hlscmafsink` elements. Works today via gst-launch subprocess.

### 18. Stream Source Failover
When primary source fails, try secondary source. Channel→stream mapping already supports multiple streams. Need retry logic in `service/vod.go` and `service/proxy.go`.

### 19. Active Stream Tracking / Connection Limits
Limited tuners per HDHR, limited connections per IPTV provider. Need tuner/connection count check BEFORE starting new pipeline. Per-source connection limits in M3U account settings.

### 20. Probe Cache Pre-Population
Without cached probe, channel switching requires live probe (2-5s delay). Probe scheduler runs on startup but must cover all channels. Consider priority probe queue for uncached channels.

### 21. Duplicated Builder Logic
`buildMPEGTSNative` and `buildNonMPEGTSNative` share identical code for:
- 10-bit/VT HEVC decode fallback (`decHW` logic)
- Output scaling (`videoscale + videoconvert + capsfilter`)

Extract to shared helpers to reduce copy-paste drift risk.

### 22. GetInit/GetSegment Goroutine Churn
`TrackStore.GetInit()` and `GetSegment()` spawn a goroutine every 500ms to broadcast on the cond var (workaround for missed signals). Over a 30s timeout that's ~60 short-lived goroutines. Replace with `time.AfterFunc` + cancel on return.

### 23. Video Codec MIME Not Derived From Init Segment
Frontend detects video codec by parsing init segment bytes (`detectVideoCodec` in app.js). This works for known codecs (avc1/hvc1/av01) but is fragile — it scans raw bytes for codec box markers. If the init segment structure changes or a new codec is added, detection breaks silently and the wrong MIME is passed to `addSourceBuffer()`.

**Fix:** Have the backend report the exact codec string (e.g. `avc1.64001f`, `hev1.1.6.L120.90`) alongside the init segment. Frontend uses that directly instead of guessing.

### 24. 10-bit Content Detection Via String Matching
**RESOLVED:** Probe now captures `BitDepth` from `AVPixFmtDescriptor.comp[0].depth` (C path) and `bits_per_raw_sample` (ffprobe path). All 5 transcode paths now use `opts.SourceBitDepth > 8` as primary check, with SourcePixFmt string matching as fallback when BitDepth is 0.

### 25. Mid-Stream Codec or Resolution Change
Live IPTV sources can change resolution (e.g. SD→HD during program change), codec, or audio layout mid-stream. GStreamer may handle this via caps renegotiation, but TrackStore's init segment is built once and never updated (except AV1 sequence header).

**Impact:** If source switches from 1080p to 720p, the init segment still declares 1080p. Browser may show corrupted video or error. If audio codec changes from AAC to AC3, the audio chain breaks.

**Fix:** Monitor for caps changes on appsink. When detected, emit new init segment with updated parameters. Increment generation counter so frontend re-initializes SourceBuffers.

## Low Priority / Future

### 26. WebRTC Sink for Sub-Second Latency
`webrtcsink` available in gstreamer:1.2. Requires signalling server (WebSocket), ICE/STUN/TURN. Transformative for live TV channel surfing.

### 27. WebM Output Needs Opus Audio
WebM container requires Opus, not AAC. `buildAudioChain` always outputs AAC. Any WebM output path will produce invalid files.

### 28. Plugin Static Pad Transcode
tvproxydemux static `video` pad works for copy but not external encode chains. Caps negotiation issue. Native tsdemux path used instead (proven working).

### 29. Audio Channels on Client Stream Profile
Add `audio_channels` field (0=passthrough, 2=stereo, 6=5.1). Defaults: Browser=2, Phone=2, Plex=0, DLNA=2. Wire into buildAudioChain capsfilter.

### 30. Audio Language Selection in Native MPEG-TS Path
Native tsdemux takes first audio pad. Need to collect all audio pads, check language tags, select preferred. tvproxydemux does this internally — replicate in native path.

### 31. DTS/TrueHD/FLAC Source Audio
Audio chain handles these codecs (decode → AAC transcode) but they're untested in the native GStreamer path. If GStreamer can't find the decoder plugin (e.g. `avdec_dca` missing), the pipeline silently produces no audio rather than erroring.

**Fix:** Check decoder plugin availability at pipeline build time. Fall back to software decode or report clear error.

### 32. AV1 in MPEG-TS Container
AV1 has no official MPEG-TS stream type. If a source delivers AV1 in MPEG-TS, tsdemux won't recognize it. The pipeline will produce video-less output.

**Fix:** Detect AV1+MPEG-TS combination at probe time and report incompatibility to user. Suggest transcoding or container change.

### 33. Subtitle/Teletext Streams Consume Memory
Demuxers expose subtitle and teletext pads. Unlinked pads get `fakesink` (`drainUnlinkedPad`), which is correct but each fakesink consumes memory for buffered data. Sources with many subtitle tracks (e.g. DVB with 10+ teletext pages) waste memory.

**Fix:** Set `fakesink` async=false (already done) and consider adding `max-size-buffers=1` to limit memory per unused stream.

## Code Review Cleanup

### 34. Dead Code Removal — DONE
Removed 1151 lines: controller.go, SC appsink functions, dead builder functions, unused native.go exports.

### 35. Unused PipelineOpts Fields — DONE
Removed DeinterlaceMethod, SourceFPS, SourceFieldOrder, SourceSampleRate, SourceColorSpace, SourceProfile, DualOutput, AudioLanguage.

### 36. Redundant Codec Normalization — DONE
Replaced media.NormalizeVideoCodec() with gstreamer.NormalizeCodec(), removed the function.

### 37. Unexport Internal Functions — DONE
PluginsAvailable → pluginsAvailable, IsMPEGTS removed (unused), DiscovererAvailable removed.

### 38. Probe Removal — DONE
Removed entire probe subsystem: avprobe package, probe scheduler, probe workers, ensureProbe, probeAsync, SkipProbe. Replaced with passive metadata cache populated from playback. ~985 lines removed.

## Reference

### Codec → GStreamer Elements
| Codec | Parser | Decoder (VT/VA/NV/SW) | Encoder (VT/VA/NV/SW) |
|-------|--------|----------------------|----------------------|
| h264 | h264parse | vtdec / vah264dec / nvh264dec / avdec_h264 | vtenc_h264 / vah264lpenc / nvh264enc / x264enc |
| h265 | h265parse | vtdec / vah265dec / nvh265dec / avdec_h265 | vtenc_h265 / vah265lpenc / nvh265enc / x265enc |
| av1 | av1parse | dav1ddec / vaav1dec / nvav1dec / avdec_av1 | vtenc_av1 / vaav1lpenc / nvav1enc / svtav1enc |
| mpeg2 | mpegvideoparse | avdec_mpeg2video / vampeg2dec | — (always transcode) |

### Audio Chains
| Codec | Chain |
|-------|-------|
| aac_latm | aacparse → avdec_aac_latm → audioconvert → audioresample → faac → aacparse |
| aac | aacparse (passthrough in VOD, transcode in live) |
| mp2 | mpegaudioparse → mpg123audiodec → audioconvert → audioresample → faac → aacparse |
| ac3 | avdec_ac3 → audioconvert → audioresample → faac → aacparse |
| eac3 | avdec_eac3 → audioconvert → audioresample → faac → aacparse |
| dts | avdec_dca → audioconvert → audioresample → faac → aacparse |
| truehd | avdec_truehd → audioconvert → audioresample → faac → aacparse |
| flac | flacparse → flacdec → audioconvert → audioresample → faac → aacparse |
| opus | opusdec → audioconvert → audioresample → faac → aacparse |

### Hardware Acceleration
| Hardware | Setting | Encoders |
|----------|---------|----------|
| Intel Arc (A380) | `vaapi` | vaav1lpenc, vah265lpenc, vah264lpenc |
| Intel Gen9+ | `vaapi` | vah265lpenc, vah264lpenc |
| NVIDIA Turing+ | `nvenc` | nvav1enc, nvh265enc, nvh264enc |
| Apple Silicon | `videotoolbox` | vtenc_h265, vtenc_h264, vtenc_av1 (M4+) |
| Software | `none` | svtav1enc, x265enc, x264enc |

### MSE Architecture (Current)
```
GStreamer pipeline → appsink (sync=false) → Go TrackStore (fMP4 via mp4ff) → HTTP segments → Browser MSE
Timeline: tfdt (BaseMediaDecodeTime) accumulated from sample durations. Go is sole source of truth.
Keyframe detection: BufferFlagDeltaUnit (GStreamer flag, not OBU/NALU parsing)
Duration: rolling 10-frame average, PTS diff primary, buf.Duration fallback
AV1: strip TD OBUs, skip ≤10 byte buffers, sequence header in av1C configOBUs
Audio: always transcode to AAC stereo for MSE, copy AAC for VOD, probe-driven channels for file output
Audio timescale: must match source sample rate (set via probe → SetAudioRate)
Audio MIME: served from MSE debug endpoint (GetAudioCodecString), frontend reads it dynamically
Deinterlace: auto-inserted when probe detects interlaced source (SourceInterlaced)
10-bit decode: falls back to SW when SourceBitDepth > 8 or PixFmt contains "10"/"12"
Seeking: CGO gst_element_seek_simple, generation counter invalidates old segments
Partner flush: audio only flushes when video flushes (keyframe + 2s boundary)
Shared base PTS: first track to produce a PTS sets the shared origin for both tracks
Probe data: bit_depth, interlaced, color_space, field_order, fps, channels, sample_rate all flow to PipelineOpts
```
