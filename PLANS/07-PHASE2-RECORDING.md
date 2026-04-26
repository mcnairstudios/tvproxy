# Phase 2: Recording Plugin — Fix the broken recording system

## Plans 07-10: Get recordings working again

### Plan 07: Create pkg/output/record/ package
- RecordingPlugin implements OutputPlugin
- Owns StreamMuxer("mp4") + file handle
- Writes to `<recorddir>/stream/<streamid>/source.mp4`
- Audio always AAC, video from `default_video_codec` setting
- PushVideo/PushAudio → conv.ToAVPacket → muxer
- Stop() → close muxer, close file
- Tests: write packets, verify mp4 file produced
- **New code only, no changes to existing files**

### Plan 08: Wire "always recording" into session manager
- Every session creates a RecordingPlugin alongside the delivery plugin
- FanOut distributes to [DeliveryPlugin, RecordingPlugin]
- Recording writes to `<recorddir>/stream/<streamid>/active/source.mp4`
- On session close (no record flag): delete the file
- On session close (record flag set): move to `<recorddir>/recordings/`
- Modify manager.go runPipeline to always attach RecordingPlugin
- **This is the critical fix — recordings start working again**

### Plan 09: Fix recording completion flow
- `completeRecording()` in vod_recording.go: use `sess.FilePath` not hardcoded "source.ts"
- Recording store: already fixed to accept .mp4/.ts/.mkv (v2.0.36)
- Verify metadata JSON written correctly
- Verify file moved from active/ to recorded/
- Test: start session → record → stop → verify file in recordings/

### Plan 10: Wire record button (short press / long press)
- Short press: mark "record from now" — FanOut.Add(RecordingPlugin) mid-stream
- Long press: mark "preserve from beginning" — rename existing temp file to recording
- Both set the session's `Recorded` flag so cleanup preserves files
- Auto-stop: EPG end time or 4-hour fallback
- Background recording continues after player close
- Frontend: implement long-press detection on record button
