FROM golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -ldflags="-s -w -X main.buildVersion=$VERSION" -o /tvproxy ./cmd/tvproxy/

FROM linuxserver/ffmpeg:latest

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    gosu \
    dtv-scan-tables \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-vaapi \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /tvproxy /usr/local/bin/tvproxy
COPY pkg/defaults/clients.json /defaults/clients.json
COPY pkg/defaults/settings.json /defaults/settings.json

RUN (usermod -l tvproxy -d /home/tvproxy ubuntu 2>/dev/null && groupmod -n tvproxy ubuntu 2>/dev/null || useradd -m -u 1000 tvproxy)

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /config

ENV PUID=1000
ENV PGID=1000
ENV TVPROXY_DB_PATH=/config/tvproxy.db
ENV TVPROXY_RECORD_DIR=/record
ENV TVPROXY_VOD_OUTPUT_DIR=/record

EXPOSE 8080

ENTRYPOINT ["entrypoint.sh"]
