# TVProxy

A media hub that connects your stream sources to your playback devices. TVProxy ingests from IPTV providers, tuners, and media servers, then delivers to any client — Jellyfin, Plex, DLNA players, VR headsets, smart TVs, or browsers — in whatever format they need.

Written in Go. Single binary. No external dependencies beyond ffmpeg (for HLS segmentation).

## How It Works

TVProxy is a media router, not a media center. It bridges inputs and outputs with format-aware processing in between:

```
Sources (inputs)                    Hub                           Clients (outputs)
─────────────────    ──────────────────────────────    ─────────────────────────
M3U playlists        Probe → Strategy → Pipeline      Jellyfin (API emulation)
Xtream Codes API     ┌─────────────────────────┐      Plex/Emby (HDHR emulation)
SAT>IP tuners        │ Copy     (zero CPU)      │      DLNA (Quest, smart TVs)
HDHomeRun devices    │ Remux    (repackage)     │      Browser (MSE/fMP4)
tvproxy-streams      │ Transcode (HW-accel)     │      Any HTTP client
VOD files            └─────────────────────────┘
```

**Client stream profiles** define what each output needs — codec, container, resolution ceiling, audio format. The strategy layer compares source probe data against the client profile and does exactly what's required. Profiles ship with sensible defaults per device but are fully user-configurable. Someone with a phone supporting AV1 can choose that over H.265, for instance.

One pipeline per channel, shared across all consumers (Kafka-style commit log model). A viewer, a recording, and a Jellyfin client watching the same channel all tail the same stream at their own offsets.

## Output Integrations

- **Jellyfin** — Full API server on port 8096. Any Jellyfin client works natively — phones, tablets, TVs, Quest, Fire Stick, Apple TV.
- **Plex / Emby** — HDHomeRun emulation with SSDP discovery. Shows up as a native DVR source with full guide data.
- **DLNA** — UPnP MediaServer for network players. VR headsets (Quest via Skybox), smart TVs (LG, Samsung, Panasonic).
- **Browser** — Built-in vanilla JS player with live TV, VOD, recording, and EPG guide. MSE playback via fMP4 segments.

## Source Integrations

- **M3U / Xtream Codes** — Import playlists or connect to Xtream APIs. Automatic periodic refresh.
- **SAT>IP** — DVB-T/T2, DVB-S/S2, DVB-C tuner integration via SAT>IP protocol.
- **HDHomeRun** — Discover and ingest from HDHR tuners on the network.
- **tvproxy-streams** — Companion server for local media libraries with inline probe data.
- **WireGuard** — Built-in VPN with per-source routing. Transparent to the pipeline via localhost proxy.

## Transcoding & Profiles

- **Source Stream Profiles** — Define transport and input settings per source type.
- **Client Stream Profiles** — Define output requirements per client. Strategy resolves copy vs transcode automatically.
- **Hardware Acceleration** — Intel QSV, VA-API, NVIDIA NVENC, Apple VideoToolbox. H.264, H.265/HEVC, AV1 encode/decode.
- **Media Pipeline** — libavformat integrated as an in-process library (via go-astiav) for probing, demuxing, decoding, encoding, and muxing. Previous iterations used ffmpeg subprocesses (orphaned process issues) and GStreamer (required custom plugins, couldn't detect a whole class of media types). ffmpeg subprocess retained for HLS segmentation only.

## Management

- **Channels** — Multi-stream failover, groups, per-channel profile overrides.
- **EPG** — XMLTV import, auto-match to channels, programme guide grid.
- **Recording** — One-click during live playback. Scheduled recordings via EPG guide. Survives restarts.
- **TMDB** — Automatic poster art, metadata, ratings, and episode info for VOD.
- **Activity** — Real-time viewer tracking and session monitoring.
- **Multi-User** — JWT auth, invite tokens, role-based access, per-user channel filtering.

## Quick Start

```bash
# Requires libavformat/libavcodec/libavutil/libavfilter/libswscale/libswresample dev libs
CGO_ENABLED=1 go build -o tvproxy ./cmd/tvproxy/
TVPROXY_BASE_URL=http://192.168.1.100 ./tvproxy
```

The server starts on port 8080. Default credentials: `admin` / `admin`.

## Docker

```bash
docker run -p 8080:8080 -p 8096:8096 -p 47601-47610:47601-47610 \
  -e TVPROXY_BASE_URL=http://192.168.1.100 \
  -v tvproxy-data:/config -v tvproxy-recordings:/record \
  gavinmcnair/tvproxy:latest
```

For hardware transcoding, pass through the GPU:

```bash
# Intel Arc / QSV / VA-API
docker run ... --device /dev/dri:/dev/dri gavinmcnair/tvproxy:latest

# NVIDIA (requires nvidia-container-toolkit)
docker run ... --gpus all gavinmcnair/tvproxy:latest
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `TVPROXY_BASE_URL` | _(required)_ | LAN base URL (e.g. `http://192.168.1.100`) |
| `TVPROXY_PORT` | `8080` | Listen port |
| `TVPROXY_RECORD_DIR` | `/record` | Recording output directory |
| `TVPROXY_JWT_SECRET` | `change-me-in-production` | JWT signing secret |
| `TVPROXY_API_KEY` | _(empty)_ | Optional API key auth |
| `TVPROXY_USER_AGENT` | `TVProxy` | Upstream request User-Agent |
| `TVPROXY_LOG_LEVEL` | `info` | Log level |

All settings configurable via the web UI. Full API documentation at `/api/docs`.

## Architecture

```
cmd/tvproxy/         — Entry point, DI wiring, routes
pkg/
  config/            — Environment-based configuration
  handler/           — HTTP handlers
  hls/               — HLS session management
  jellyfin/          — Jellyfin API server emulation
  lib/av/            — libavformat wrappers (probe, demux, decode, encode, mux, filter, scale)
  session/           — Kafka-style session manager (one pipeline per channel, consumer tracking)
  service/           — Business logic (strategy, proxy, VOD, M3U, EPG, recording)
  store/             — JSON-backed in-memory stores
  models/            — Data models
  worker/            — Background workers (refresh, SSDP, HDHR, scheduler)
  wireguard/         — WireGuard tunnel management
  tmdb/              — TMDB metadata client and image proxy
  xtream/            — Xtream Codes API client
  logocache/         — Image caching proxy
web/dist/            — Vanilla JS SPA (embedded via Go embed.FS)
```

## Development

```bash
make build          # Local binary
make test           # Run all tests
make docker-build   # Multi-arch build (amd64+arm64) and push
make docker-local   # Build for current arch only
```

## Acknowledgements

Inspired by [Threadfin](https://github.com/Threadfin/Threadfin) and [Dispatcharr](https://github.com/Dispatcharr/Dispatcharr).
