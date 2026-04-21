# Concerns (non-blocking, review later)

## Watcher segment index sort
`watcher.go:segmentIndex.Add()` calls `sort.Strings()` on every segment add. O(n log n) per segment. Fine for current scale. If streams run for many hours (3600+ segments), could switch to append-only since GStreamer writes in order.

## Segment reads hit disk per request
`mse.go:Segment()` calls `os.ReadFile()` on every HTTP request. No in-memory caching. OS page cache handles this for 1-2 users. If multi-user performance matters later, consider mmap or read-once cache.

## source.ts filename extension
`pipeline_builder.go:addRawRecording()` always writes `source.ts`. For MKV/MP4 VOD sources, the extension is wrong. Content is valid regardless — just confusing if someone inspects with ffprobe.

## ConnectDynamicPads implied knowledge
`executor.go:ConnectDynamicPads()` exists but the PipelineSpec doesn't declare which elements need dynamic pad handling. The session manager must know to call it for demuxers. Could add a `DynamicPads` field to ElementSpec if this becomes error-prone.

## Segment 404 vs long-poll
Old TrackStore waited 10 seconds for segments (cond.Broadcast). New MSE handler returns 404 immediately. Frontend worker retries with backoff. Simpler but slightly higher latency on segment delivery. Monitor if users notice buffering gaps.

## Protobuf vs simpler format
probe.pb uses protobuf-c (C side) + google.golang.org/protobuf (Go side). Works but adds dependencies for ~10 fields. If protobuf becomes a build burden, a fixed-size binary struct or even JSON would work for this scale.
