# TVProxy Architecture Design

## Media Cloud (Core)

### Media Store
- Backed by bolt — fast key-value lookups
- Contains only key information about streams needed for channel lists, groupings, quick display
- Playback-time metadata (probe data, etc.) loaded ad-hoc, not stored long-term in the store

### Caching Layers
- EPG data, live program info
- TMDB enrichment (posters, synopsis, cast, etc.)
- Together with the store, forms the "media cloud" — abstract, encapsulated
- Knows the source of every piece of media, how to deliver it, and all cached enrichment

## Input Side — Source Plugins

Plugins provide streams into the media cloud. Extensible by design.

### Current plugins:
- SAT>IP
- HDHomeRun
- IPTV (M3U)
- IPTV (Xtream)
- TVProxy Streams (curated, pre-enriched M3U with full metadata: season, episode, resolution, codec)

### Future plugins (examples):
- HDMI capture (URayTech encoder)
- Free streaming: Samsung TV Plus, LG Channels, Pluto TV, Twitch
- Apple Trailers
- RTSP cameras
- Any video source

### Source Stream Profiles
- Configuration per source type
- Normalize any input into a consistent internal format
- Whatever protocol/format comes in, the source profile defines how to ingest it
- Rest of the system sees one uniform stream

## Output Side

### Transcoding Out
- Symmetric to input: generic transcode on the way out, appropriate for the target client
- Client detection recognises who's asking → applies a client stream profile
- Client stream profile defines codec, resolution, container, delivery method

### Output Plugins (Delivery Layer)
- Reusable transport mechanisms: MSE, HLS, DASH, raw stream, WebRTC (future)
- Extensible — new delivery methods added as plugins
- Client profile says "what" (codec, resolution, container), output plugin handles "how"
- Multiple client profiles can share the same output plugin

### Output Clients
- Configuration-only: match rules + output plugin + delivery format
- Easy to add without code changes

#### Examples:
- **HDHR emulation** → Plex, Jellyfin, Channels DVR
- **DLNA** → LG TV, Samsung TV, Oculus Quest, Panasonic TV
- **Browser UI** → tvproxy's own player (modular, replaceable)
- **Jellyfin emulation** → FireTV, Swiftfin, Apple TV
- **M3U + XMLTV output** → other IPTV players, other tvproxy instances

### Browser UI
- Two parts:
  1. **Media viewing** — modular, replaceable, just another output client. Could be swapped for a React app, native app, etc.
  2. **Admin/config** — browser-based, more fixed. tvproxy-specific.
- Admin login = config access. User login = their channels and media.
- Could be split into separate apps in future. Both consume the same API.

### Jellyfin
- Just another output client — same API calls as browser UI, translated through Jellyfin plugin layer
- Not a special case, just a different view onto the media cloud

## Content Organisation

### TVProxy Streams (Curated Library)
- Pre-enriched VOD from tvproxy-streams server
- Full metadata at source (season, episode, resolution, codec, etc.)
- Presented to Jellyfin as Movies and TV Series
- This is the well-curated content for devices that can't handle millions of streams

### Channels & Favorites (User-Curated)
- Built from ANY stream from any source
- User-organised, mapped to EPG sources
- Shown alongside the curated library in all output clients (Jellyfin, DLNA, etc.)
- Two tiers: curated VOD library + user-organised channels

## Recording Architecture

### Core Principle: Every Playback Always Records
- When you watch something, it ALWAYS writes to `<recorddir>/stream/<streamid>/`
- Delivery to client (HLS/MSE/DASH/etc.) runs in parallel off the same decode
- Recording is not a special mode — it's the default behaviour
- One decode → fan out to recording sink + delivery sink

### On Player Close
- Stream file is **deleted** by default
- UNLESS the user pressed the record button → file is preserved

### Manual Recording (Record Button)
- **Short press** → records from NOW forward
- **Long press** → preserves from the BEGINNING of the session (entire session)
- Output goes to `<recorddir>/recordings/` with:
  - Media file (mp4/aac, video codec from `default_video_codec` setting)
  - Metadata JSON file (same basename)
- If user closes window while recording → recording continues in background
- Auto-stop:
  - If EPG data available → stop at end of current program
  - Otherwise → 4 hour maximum fallback
- Stop recording → file is preserved in recordings directory

### Scheduled Recording (from EPG)
- User schedules from EPG guide
- Starts at program start time, stops at program end time
- Same output path and format as manual recordings
- Same mechanism — just triggered by the scheduler instead of user button press

### Recording Format
- Container: mp4
- Audio: AAC
- Video codec: from `default_video_codec` setting (copy = source codec, or h264/h265/av1)
- The recording is ALWAYS in this format, regardless of what the client sees
- Recording is treated as an input source for playback — it just happens to be local on disk
- When playing back a recording, it goes through the normal output pipeline (client profile → output plugin → delivery)
