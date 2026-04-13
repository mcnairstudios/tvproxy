FROM golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    libavformat-dev \
    libavcodec-dev \
    libavutil-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -ldflags="-s -w -X main.buildVersion=$VERSION" -o /tvproxy ./cmd/tvproxy/

FROM gavinmcnair/gstreamer:1.1

COPY --from=builder /tvproxy /usr/local/bin/tvproxy
COPY pkg/defaults/clients.json /defaults/clients.json
COPY pkg/defaults/settings.json /defaults/settings.json

RUN (usermod -l tvproxy -d /home/tvproxy ubuntu 2>/dev/null && groupmod -n tvproxy ubuntu 2>/dev/null || useradd -m -u 1000 tvproxy)

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /config

RUN mkdir -p /run/user/1000 && chmod 700 /run/user/1000

ENV PUID=1000
ENV PGID=1000
ENV XDG_RUNTIME_DIR=/run/user/1000
ENV LIBVA_DRIVER_NAME=iHD
ENV TVPROXY_DB_PATH=/config/tvproxy.db
ENV TVPROXY_RECORD_DIR=/record
ENV TVPROXY_VOD_OUTPUT_DIR=/record
ENV GST_PLUGIN_PATH=/usr/local/lib/gstreamer-1.0

EXPOSE 8080

ENTRYPOINT ["entrypoint.sh"]
