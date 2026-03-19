#!/bin/bash
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

# If a GPU render device exists, run as its group so ffmpeg can access it
if [ -e /dev/dri/renderD128 ]; then
  PGID=$(stat -c '%g' /dev/dri/renderD128)
fi

# Update tvproxy group/user to match requested IDs
if [ "$(id -g tvproxy)" != "$PGID" ]; then
  groupmod -o -g "$PGID" tvproxy
fi
if [ "$(id -u tvproxy)" != "$PUID" ]; then
  usermod -o -u "$PUID" tvproxy
fi

# Ensure /data and /record are writable
chown "$PUID:$PGID" /data
mkdir -p /record
chown "$PUID:$PGID" /record

exec gosu tvproxy tvproxy "$@"
