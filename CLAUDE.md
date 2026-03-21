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
  database/             — SQLite connection, sequential migrations (applied on startup)
  ffmpeg/               — stream profile arg composition (compose.go)
  handler/              — HTTP handlers + integration_test.go (mirrors main.go wiring)
  m3u/                  — M3U playlist parser
  middleware/           — JWT auth middleware
  models/               — all data models (single file: models.go)
  openapi/              — OpenAPI spec
  repository/           — database access layer (one file per entity)
  service/              — business logic (proxy, auth, m3u refresh, EPG, HDHR, etc.)
  worker/               — background workers (M3U refresh, EPG refresh, SSDP, HDHR discovery)
  xmltv/                — XMLTV EPG parser
  xtream/               — Xtream Codes API client
web/dist/app.js         — vanilla JS SPA (single file, embedded via Go embed.FS)
entrypoint.sh           — Docker entrypoint (UID/GID handling, GPU device group detection)
```

## Key Architecture

- **DI**: Manual injection — main.go wires repos -> services -> handlers. No framework.
- **Database**: SQLite via modernc.org/sqlite (pure Go, no CGO). Migrations are sequential in pkg/database/migrations.go, applied on startup. Supports both SQL and Go-function migrations.
- **Frontend**: Single vanilla JS file (web/dist/app.js). No build step. Embedded via Go's embed.FS.
- **Stream profiles**: Dropdown-driven composition (source_type + hwaccel + video_codec + container -> custom_args). `custom_args` is the single source of truth — dropdowns compose initial args, users can then edit directly. Composition logic in pkg/ffmpeg/compose.go.
- **Proxy**: Profile resolution chain: (1) `?profile=Name` query param, (2) client header detection, (3) default "proxy" fallback. If custom_args is empty (Direct profiles), uses HTTP passthrough. Otherwise spawns ffmpeg with the stored args.
- **Client detection**: Generic, data-driven header matching. Users define "clients" with HTTP header match rules (any header, any pattern). Each client auto-creates a linked stream profile. Match engine checks rules in priority order (AND logic per client). Zero hardcoded header analysis — all matching driven by database rows. Code: pkg/service/client.go, pkg/repository/client.go, pkg/handler/client.go.
- **Stream profile categories**: `is_system` (Direct, Proxy — undeletable, uneditable), `is_client` (auto-created per client — undeletable via API, editable, removed when parent client is deleted), regular (user-created, fully editable). List sorts: system first, client second, regular last (alphabetical within each).
- **Migration seeds**: 1 VLC user agent, 2 system stream profiles (Direct, Proxy) + 5 regular (Browser, SAT>IP Copy, M3U Copy, M3U->MP4, M3U->Matroska) + 3 client-detection profiles (Plex, VLC, Browser) with 3 seeded clients. Default profile is Proxy.

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
