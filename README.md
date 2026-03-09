# TVProxy

IPTV stream management and proxy server written in Go. Consolidates IPTV sources (M3U/Xtream Codes), manages channels and EPG data, proxies streams, and emulates HDHomeRun devices for Plex/Emby/Jellyfin integration.

## Features

- **Stream Management** - Import and manage M3U playlists and Xtream Codes accounts with automatic periodic refresh
- **Channel Management** - Create channels with multi-stream failover, organize into groups
- **EPG Support** - Import XMLTV EPG sources, auto-match programs to channels
- **Stream Proxy** - Fan-out proxy with per-channel connection sharing and automatic failover
- **HDHomeRun Emulation** - Emulates HDHomeRun devices for native Plex/Emby/Jellyfin DVR integration
- **Output Generation** - Serves M3U playlists and XMLTV EPG for any IPTV player
- **Web Interface** - Built-in web UI for managing all aspects of the system
- **Authentication** - JWT-based auth with optional API key support
- **Single Binary** - Everything including the web UI is embedded in a single Go binary
- **SQLite Database** - No external database dependencies required

## Quick Start

```bash
go build ./cmd/tvproxy/
./tvproxy
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
| `TVPROXY_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `TVPROXY_LOG_JSON` | `false` | JSON log output |
| `TVPROXY_M3U_REFRESH_INTERVAL` | `24h` | M3U auto-refresh interval |
| `TVPROXY_EPG_REFRESH_INTERVAL` | `12h` | EPG auto-refresh interval |

## Docker

```bash
docker build -t tvproxy .
docker run -p 8080:8080 -v tvproxy-data:/data tvproxy
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
- `POST /api/channels/{id}/streams` - Assign streams to channel

### Channel Groups
- `GET/POST /api/channel-groups` - List/create groups
- `GET/PUT/DELETE /api/channel-groups/{id}` - Manage group

### EPG
- `GET/POST /api/epg/sources` - List/create EPG sources
- `GET/PUT/DELETE /api/epg/sources/{id}` - Manage source
- `POST /api/epg/sources/{id}/refresh` - Refresh EPG source
- `GET /api/epg/data` - List EPG data

### HDHomeRun Devices
- `GET/POST /api/hdhr/devices` - List/create devices
- `GET/PUT/DELETE /api/hdhr/devices/{id}` - Manage device

### Settings
- `GET/PUT /api/settings` - Get/update application settings

### Output (no auth required)
- `GET /output/m3u` - Generated M3U playlist
- `GET /output/epg` - Generated XMLTV EPG

### HDHomeRun (no auth required)
- `GET /hdhr/discover.json` - Device discovery
- `GET /hdhr/lineup.json` - Channel lineup
- `GET /hdhr/lineup_status.json` - Lineup status
- `GET /hdhr/device.xml` - Device description

### Stream Proxy (no auth required)
- `GET /proxy/stream/{channelID}` - Proxy stream for channel

### OpenAPI
- `GET /api/openapi.yaml` - OpenAPI 3.0 specification

## Development

```bash
# Run tests
go test ./...

# Build
go build ./cmd/tvproxy/

# Run
./tvproxy
```

## Architecture

TVProxy follows a clean layered architecture:

- **`cmd/tvproxy/`** - Application entry point and dependency wiring
- **`pkg/config/`** - Environment-based configuration
- **`pkg/database/`** - SQLite connection and migrations
- **`pkg/models/`** - Domain structs
- **`pkg/repository/`** - Data access layer
- **`pkg/service/`** - Business logic layer
- **`pkg/handler/`** - HTTP handlers
- **`pkg/middleware/`** - Auth, logging, recovery middleware
- **`pkg/worker/`** - Background refresh workers
- **`pkg/m3u/`** - M3U playlist parser
- **`pkg/xmltv/`** - XMLTV/EPG parser
- **`pkg/xtream/`** - Xtream Codes API client
- **`web/`** - Embedded web frontend
