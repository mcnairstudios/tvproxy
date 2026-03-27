# TVProxy

IPTV stream management and proxy server written in Go. Inspired by Dispatcharr and Threadfin. Manages channels, streams (M3U and SAT>IP), EPG data, and emulates HDHomeRun devices for Plex/Emby/Jellyfin. SAT>IP stream support is complete; integration testing is ongoing.

## Build & Test

```bash
make build          # Local binary
make test           # Run all tests
make docker-build   # Multi-arch build (amd64+arm64) and push to Docker Hub
make docker-local   # Build for current arch only (no push)
make run            # docker compose up -d
make logs           # docker compose logs -f
```

## Docker — ALWAYS use `make docker-build` to build and push

- Registry: **Docker Hub `gavinmcnair/tvproxy`**
- Multi-arch: **linux/amd64 + linux/arm64** (buildx builder: `mybuilder`)
- Base image: `linuxserver/ffmpeg:latest` (ffmpeg 8.x with QSV/VA-API/NVENC/VideoToolbox)
- Port mapping: 8081 (host) -> 8080 (container) — port 8080 is used by artportfolio on the host
- Target server is amd64; dev machine is arm64 (Mac). Always build both platforms.

## Project Structure

```
cmd/tvproxy/main.go     — entry point, DI wiring, router setup
pkg/
  config/               — env-based config
  database/             — SQLite connection (migration tracking only)
  defaults/             — JSON client/settings defaults (embedded, mountable override at /config/)
  ffmpeg/               — stream profile arg composition (compose.go)
  handler/              — HTTP handlers + integration_test.go (mirrors main.go wiring)
  hls/                  — HLS downstream remuxer (mp4→HLS segments for Safari/iPhone)
  httputil/             — shared HTTP utilities (headers, fetch, decompress)
  logocache/            — logo caching proxy (GET /logo?url= endpoint)
  m3u/                  — M3U playlist parser
  middleware/           — JWT auth middleware
  models/               — all data models (single file: models.go)
  openapi/              — OpenAPI spec
  service/              — business logic (proxy, auth, m3u, EPG, HDHR, reset, backup, etc.)
  session/              — Kafka-style session manager (ffmpeg lifecycle, consumer tracking, file tailing)
  store/                — JSON-backed in-memory stores (one per entity + streams.gob + epg.gob)
  worker/               — background workers (M3U refresh, EPG refresh, SSDP, HDHR discovery)
  xmltv/                — XMLTV EPG parser
  xtream/               — Xtream Codes API client
web/dist/app.js         — vanilla JS SPA (single file, embedded via Go embed.FS)
entrypoint.sh           — Docker entrypoint (UID/GID, GPU detection, /defaults→/config copy)
```

## Key Architecture

- **DI**: Manual injection — main.go wires stores -> services -> handlers. No framework.
- **Data Storage**: All data in JSON files in /config directory. SQLite retained only for migration tracking table. pkg/repository/ deleted. All CRUD through pkg/store/ interfaces.
- **Docker paths**: /defaults (embedded defaults), /config (active data, mounted volume), /record (recordings).
- **Frontend**: Single vanilla JS file (web/dist/app.js). No build step. Embedded via Go's embed.FS.
- **Stream profiles**: Dropdown-driven composition (source_type + hwaccel + video_codec + container -> custom_args). `custom_args` is the single source of truth — dropdowns compose initial args, users can then edit directly. Composition logic in pkg/ffmpeg/compose.go.
- **Proxy**: Profile resolution chain: (1) `?profile=Name` query param, (2) client header detection, (3) default "proxy" fallback. If custom_args is empty (Direct profiles), uses HTTP passthrough. Otherwise spawns ffmpeg with the stored args.
- **Client detection**: Generic, data-driven header matching. Users define "clients" with HTTP header match rules (any header, any pattern). Each client auto-creates a linked stream profile. Match engine checks rules in priority order (AND logic per client). Zero hardcoded header analysis — all matching driven by database rows. Code: pkg/service/client.go, pkg/repository/client.go, pkg/handler/client.go.
- **Stream profile categories**: `is_system` (Direct, Proxy — undeletable, uneditable), `is_client` (auto-created per client — undeletable via API, editable, removed when parent client is deleted), regular (user-created, fully editable). List sorts: system first, client second, regular last (alphabetical within each).
- **Client defaults**: JSON-driven (`pkg/defaults/clients.json`, embedded via `//go:embed`). On fresh install or reset, seeds clients (Plex, VLC, Skybox, 4XVR, Browser, etc.) with linked stream profiles. Users can override by mounting their own `clients.json` at `/data/clients.json`. Only seeds if clients table is empty.
- **Migration seeds**: 2 system stream profiles (Direct, Proxy) seeded in `seedData()`. Client seeding is separate via `SeedClientDefaults()`. Default profile is Proxy.

## Session & Recording Design (deterministic rules)

These rules are absolute. Any code change MUST comply. If a rule conflicts with an implementation, the rule wins.

1. **One upstream ffmpeg per channel, max.** Sessions keyed by channelID. If a session exists, reuse it. Never start a second upstream ffmpeg for the same channel. Downstream consumer transcodes (e.g., remux to mpegts for Plex) are separate processes reading from the file — as many as needed.
2. **Zero ffmpegs when possible.** If source codec matches target codec and no processing (deinterlace, fps_mode, filters) is requested, use `-c copy`. Probe source on session start to decide.
3. **The recording scheduler owns all ffmpeg sessions.** Single entry point for creating, tracking, and cleaning up sessions. No separate VOD session management. Direct/Proxy modes are lightweight HTTP pipes outside the scheduler.
4. **Every ffmpeg session is a recording.** Browser playback, DLNA, scheduled — all the same. FFmpeg starts, writes to a file, consumers tail it.
5. **Reference counting (inode model).** Each session has a consumer count. Browser viewer = consumer. DLNA viewer = consumer. Active recording = virtual consumer. Downstream transcoder = consumer. Count hits zero → cleanup. No exceptions, no special flags.
6. **Consumers never control the session.** They register, tail the file, and deregister. They don't start or stop the upstream ffmpeg. If a consumer needs a different format, it spawns its own downstream ffmpeg reading from the session file.
7. **Click record = add a virtual consumer.** File output is preserved when the recording consumer disconnects. Stop recording = remove the recording consumer.
8. **No segments, no extraction.** Click record, get the full output. No start/stop cutting, no second ffmpeg for extraction. Post-processing is the user's problem.
9. **One code path.** No special cases for scheduled vs watching vs recording. Same function, same logic, every time. Different options, one set of code.
10. **`default_video_codec` setting.** System-wide codec (copy, h264, h265, av1). Scheduled recordings compose args from this on the fly. Browser profile tracks the default but can be overridden. Configured in Settings UI "Encoding Defaults" section alongside hwaccel.
11. **`default_hwaccel` setting.** System-wide hardware acceleration. Updates Browser profile when changed. Configured in Settings UI "Encoding Defaults" section alongside video codec.
12. **Container is always mp4** for ffmpeg sessions (seekable, browser-compatible, recording-friendly).
13. **Recording and Copy system profiles removed.** Only Direct and Proxy remain as system profiles. All encoding driven by settings + client profiles.
    - **Direct**: raw URLs in the output M3U. Client connects to source. No proxy, no tunnel, nothing.
    - **Proxy**: HTTP passthrough. Goes through WireGuard tunnel if configured. No ffmpeg.
    - **FFmpeg sessions**: upstream capture writes mp4 to file. Consumers tail it or spawn downstream transcodes.
14. **Codec auto-detect on session start.** Probe source, compare to target. Match + no filters = `-c copy`. Match + filters = decode+re-encode same codec. Mismatch = transcode.
15. **Recording codec depends on who started the session first.** Browser started → browser's codec. Scheduler started → default_video_codec. Acceptable trade-off.
16. **Never fight, never re-transcode.** Open a channel already running → get whatever it's producing. No negotiation.
17. **Kafka-style commit log model.** Each channel is a topic. FFmpeg is the single producer writing an append-only log (fragmented mp4 file). Consumers (viewers, recordings, transcoders) read at their own offsets via TailReader. The file IS the multiplexer. Implementation in `pkg/session/` — Manager, Session, Consumer, TailReader.

## Common Pitfalls

- **Test counts**: Integration tests assert exact counts. Adding seeded data in migrations WILL break tests — always update expected counts in integration_test.go.
- **Migration IDs**: Stream profile IDs in tests are affected by SQLite AUTOINCREMENT — deleted seed rows leave gaps. Check actual IDs when updating test assertions.
- **Docker UID**: linuxserver/ffmpeg base has `ubuntu` user at UID 1000. Dockerfile renames it to `tvproxy`.
- **GPU access**: entrypoint.sh auto-detects /dev/dri/renderD128 group and sets PGID to match. No manual config needed.
- **ffmpeg 8.x**: `-vaapi_device` was removed. Use `-init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va` instead.
- **AV1 + MPEG-TS**: Doesn't work (no official stream type). Use matroska, mp4, or webm container for AV1.
- **WebM**: Requires Opus audio (`-c:a libopus`), not AAC.
- **Channel number**: UNIQUE constraint. Handler returns 409 on conflict.
- **EPG data volume**: ~2000 EPG channels, ~137k programs. Bulk insert with 5000-item batches.
