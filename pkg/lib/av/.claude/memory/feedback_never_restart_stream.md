---
name: Never restart stream on seek
description: Seek must happen in-place on the existing FormatContext — never close and reopen the connection
type: feedback
---

Never restart the stream/demuxer on seek. Seek in-place using RequestSeek (which runs on the read goroutine). The FormatContext, HTTP connection, and demuxer stay alive. Only the muxer state needs resetting (flush fragment, reset DTS, bump watcher generation). Init segments don't change.

**Why:** Reopening the connection re-probes, re-tunes (SAT>IP), and adds seconds of delay. The user explicitly said "unless we negotiate it on seek we never restart the stream."

**How to apply:** All seek paths must use the in-place RequestSeek channel, never RestartWithSeek with a new DemuxSession.
