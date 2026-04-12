#!/bin/bash
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

if [ -e /dev/dri/renderD128 ]; then
  PGID=$(stat -c '%g' /dev/dri/renderD128)
fi

if [ "$(id -g tvproxy)" != "$PGID" ]; then
  groupmod -o -g "$PGID" tvproxy
fi
if [ "$(id -u tvproxy)" != "$PUID" ]; then
  usermod -o -u "$PUID" tvproxy
fi

mkdir -p /config /record /run/user/$PUID
chown "$PUID:$PGID" /config /record /run/user/$PUID
export XDG_RUNTIME_DIR=/run/user/$PUID

for f in /defaults/*.json; do
  base=$(basename "$f")
  [ -f "/config/$base" ] || cp "$f" "/config/$base"
done

exec gosu tvproxy tvproxy "$@"
