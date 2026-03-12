# TVProxy

> **Work in Progress** - TVProxy is under active development. Features may be incomplete or change without notice.

IPTV stream management and proxy server written in Go. Consolidates IPTV sources (M3U/Xtream Codes), manages channels and EPG data, proxies streams, and emulates HDHomeRun devices for Plex/Emby/Jellyfin integration.

## Features

- **Stream Management** - Import and manage M3U playlists and Xtream Codes accounts with automatic periodic refresh
- **Channel Management** - Create channels with multi-stream failover, organize into groups
- **Stream Profiles** - Configurable transcoding profiles using ffmpeg (hardware-accelerated QSV/VA-API/NVENC/VideoToolbox supported), direct passthrough, or browser-friendly output
- **Channel Profiles** - Assign stream profiles to groups of channels for consistent transcoding behavior
- **EPG Support** - Import XMLTV EPG sources, auto-match programs to channels
- **Client Detection** - Automatic player detection (Plex, VLC, Browser, user-defined) via HTTP header matching with per-client stream profiles
- **Stream Proxy** - Fan-out proxy with per-channel connection sharing and automatic failover
- **HDHomeRun Emulation** - Emulates multiple HDHomeRun devices, each on its own port, for native Plex/Emby/Jellyfin DVR integration. Includes SSDP and UDP discovery.
- **Output Generation** - Serves M3U playlists and XMLTV EPG for any IPTV player
- **Web Interface** - Built-in web UI with EPG guide grid, collapsible stream groups, in-browser playback, and data cache status indicators
- **Authentication** - JWT-based auth with optional API key support
- **Single Binary** - Everything including the web UI is embedded in a single Go binary
- **SQLite Database** - No external database dependencies required

## Quick Start

```bash
go build ./cmd/tvproxy/
TVPROXY_BASE_URL=http://192.168.1.100 ./tvproxy
```

The server starts on `http://0.0.0.0:8080` by default. On first run, a default admin user is created:
- **Username:** `admin`
- **Password:** `admin`

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `TVPROXY_HOST` | `0.0.0.0` | Listen host |
| `TVPROXY_PORT` | `8080` | Listen port |
| `TVPROXY_DB_PATH` | `tvproxy.db` | SQLite database path |
| `TVPROXY_JWT_SECRET` | `change-me-in-production` | JWT signing secret |
| `TVPROXY_API_KEY` | _(empty)_ | Optional API key for X-API-Key header auth |
| `TVPROXY_BASE_URL` | _(required)_ | Base URL without port (e.g. `http://192.168.1.100`). Server port and per-device HDHR ports are appended automatically. |
| `TVPROXY_USER_AGENT` | `TVProxy` | User-Agent header sent on upstream requests |
| `TVPROXY_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `TVPROXY_LOG_JSON` | `false` | JSON log output |
| `TVPROXY_M3U_REFRESH_INTERVAL` | `24h` | M3U auto-refresh interval |
| `TVPROXY_EPG_REFRESH_INTERVAL` | `12h` | EPG auto-refresh interval |

## Docker

```bash
docker run -p 8080:8080 -p 47601-47610:47601-47610 \
  -e TVPROXY_BASE_URL=http://192.168.1.100 \
  -v tvproxy-data:/data \
  gavinmcnair/tvproxy:latest
```

Each HDHR device gets its own port starting at 47601. Expose a range of ports to support multiple devices.

For hardware-accelerated transcoding (Intel Arc/QSV or NVIDIA), pass through the GPU devices:

```bash
# Intel Arc / QSV
docker run -p 8080:8080 -p 47601-47610:47601-47610 \
  -e TVPROXY_BASE_URL=http://192.168.1.100 \
  -v tvproxy-data:/data --device /dev/dri:/dev/dri \
  gavinmcnair/tvproxy:latest

# NVIDIA (requires nvidia-container-toolkit)
docker run -p 8080:8080 -p 47601-47610:47601-47610 \
  -e TVPROXY_BASE_URL=http://192.168.1.100 \
  -v tvproxy-data:/data --gpus all \
  gavinmcnair/tvproxy:latest
```

Or use the provided `docker-compose.yml`:

```bash
docker compose up -d
```

## API Endpoints

### Authentication
- `POST /api/auth/login` - Login with username/password
- `POST /api/auth/refresh` - Refresh access token
- `POST /api/auth/logout` - Logout
- `GET /api/auth/me` - Get current user

### Users (admin only)
- `GET/POST /api/users` - List/create users
- `GET/PUT/DELETE /api/users/{id}` - Manage user

### M3U Accounts
- `GET/POST /api/m3u/accounts` - List/create accounts
- `GET/PUT/DELETE /api/m3u/accounts/{id}` - Manage account
- `POST /api/m3u/accounts/{id}/refresh` - Refresh account streams

### Streams
- `GET /api/streams` - List streams (optional `?account_id=` filter)
- `GET/DELETE /api/streams/{id}` - Get/delete stream

### Channels
- `GET/POST /api/channels` - List/create channels
- `GET/PUT/DELETE /api/channels/{id}` - Manage channel
- `GET /api/channels/{id}/streams` - Get assigned streams
- `POST /api/channels/{id}/streams` - Assign streams to channel

### Channel Groups
- `GET/POST /api/channel-groups` - List/create groups
- `GET/PUT/DELETE /api/channel-groups/{id}` - Manage group

### Channel Profiles
- `GET/POST /api/channel-profiles` - List/create channel profiles
- `GET/PUT/DELETE /api/channel-profiles/{id}` - Manage channel profile

### Stream Profiles
- `GET/POST /api/stream-profiles` - List/create stream profiles
- `GET/PUT/DELETE /api/stream-profiles/{id}` - Manage stream profile

### Logos
- `GET/POST /api/logos` - List/create logos
- `GET/DELETE /api/logos/{id}` - Get/delete logo

### Clients
- `GET/POST /api/clients` - List/create clients
- `GET/PUT/DELETE /api/clients/{id}` - Manage client

### EPG
- `GET/POST /api/epg/sources` - List/create EPG sources
- `GET/PUT/DELETE /api/epg/sources/{id}` - Manage source
- `POST /api/epg/sources/{id}/refresh` - Refresh EPG source
- `GET /api/epg/data` - List EPG data (add `?programs=true` for full program listings)
- `GET /api/epg/now?channel_id=` - Current program for a channel
- `GET /api/epg/guide?hours=6&start=ISO` - EPG guide grid data

### HDHomeRun Devices
- `GET/POST /api/hdhr/devices` - List/create devices
- `GET/PUT/DELETE /api/hdhr/devices/{id}` - Manage device

### Settings
- `GET/PUT /api/settings` - Get/update application settings

### Output (no auth required)
- `GET /output/m3u` - Generated M3U playlist
- `GET /output/epg` - Generated XMLTV EPG

### HDHomeRun (no auth required)
These are served on the main port and on each device's dedicated port:
- `GET /discover.json` - Device discovery
- `GET /lineup.json` - Channel lineup
- `GET /lineup_status.json` - Lineup status
- `GET /device.xml` - Device description

### Stream Proxy (no auth required)
- `GET /channel/{channelID}` - Proxy stream for channel
- `GET /stream/{streamID}` - Direct stream proxy

### OpenAPI
- `GET /api/openapi.yaml` - OpenAPI 3.0 specification

## Development

```bash
make build          # Local binary
make test           # Run all tests
make docker-build   # Multi-arch build (amd64+arm64) and push to Docker Hub
make docker-local   # Build for current arch only (no push)
make run            # docker compose up -d
make logs           # docker compose logs -f
```

## Architecture

TVProxy follows a clean layered architecture:

- **`cmd/tvproxy/`** - Application entry point and dependency wiring
- **`pkg/config/`** - Environment-based configuration
- **`pkg/database/`** - SQLite connection and migrations
- **`pkg/ffmpeg/`** - Stream profile ffmpeg argument composition
- **`pkg/models/`** - Domain structs
- **`pkg/repository/`** - Data access layer
- **`pkg/service/`** - Business logic layer
- **`pkg/handler/`** - HTTP handlers
- **`pkg/middleware/`** - Auth, logging, recovery middleware
- **`pkg/worker/`** - Background workers (M3U/EPG refresh, SSDP, HDHR discovery, per-device HTTP servers)
- **`pkg/m3u/`** - M3U playlist parser
- **`pkg/xmltv/`** - XMLTV/EPG parser
- **`pkg/xtream/`** - Xtream Codes API client
- **`web/`** - Embedded web frontend

## Acknowledgements

TVProxy is inspired by and builds upon ideas from these excellent projects:

- **[Threadfin](https://github.com/Threadfin/Threadfin)** - HDHomeRun emulation and SSDP discovery approach. Thank you to the Threadfin maintainers for their work on making IPTV streams accessible to media servers.
- **[Dispatcharr](https://github.com/Dispatcharr/Dispatcharr)** - The original IPTV management platform that TVProxy is modelled after. Thank you to the Dispatcharr team for the feature design and workflow that inspired this project.

We are grateful to the maintainers and contributors of both projects for their work in the IPTV community.
