FROM linuxserver/ffmpeg:8.0.1 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    gcc \
    libc6-dev \
    make \
    nasm \
    xz-utils \
    wget \
    && rm -rf /var/lib/apt/lists/*

RUN wget -q https://ffmpeg.org/releases/ffmpeg-8.0.1.tar.xz \
    && tar xf ffmpeg-8.0.1.tar.xz \
    && cd ffmpeg-8.0.1 \
    && ./configure --disable-programs --disable-doc --enable-shared --disable-static \
       --enable-gpl --enable-version3 \
    && make -j$(nproc) \
    && make install \
    && ldconfig \
    && cd .. && rm -rf ffmpeg-8.0.1 ffmpeg-8.0.1.tar.xz

RUN wget -q https://go.dev/dl/go1.26.1.linux-$(dpkg --print-architecture).tar.gz \
    && tar -C /usr/local -xzf go1.26.1.linux-$(dpkg --print-architecture).tar.gz \
    && rm go1.26.1.linux-$(dpkg --print-architecture).tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG GOMAXPROCS=0
RUN GOMAXPROCS=$GOMAXPROCS CGO_ENABLED=1 go build -ldflags="-s -w -X main.buildVersion=$VERSION" -o /tvproxy ./cmd/tvproxy/

FROM linuxserver/ffmpeg:8.0.1

ARG TARGETARCH

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        gosu \
        dtv-scan-tables \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Intel VAAPI/QSV: Arc A380 (DG2), Alder Lake+, older Gen8-11
# intel-media-va-driver-non-free: iHD driver with encode support (Arc, Gen8+)
# i965-va-driver-shaders: legacy i965 driver for Gen5-7
# libvpl2: OneVPL dispatcher for QSV on Gen12+ (Alder Lake, Arc)
# libmfx1: MSDK dispatcher for QSV on Gen8-11
# Only available on amd64 — arm64 has no Intel GPU support
RUN if [ "$TARGETARCH" = "amd64" ]; then \
      apt-get update && apt-get install -y --no-install-recommends \
        intel-media-va-driver-non-free \
        i965-va-driver-shaders \
      && (apt-get install -y --no-install-recommends libvpl2 2>/dev/null || true) \
      && (apt-get install -y --no-install-recommends libmfx1 2>/dev/null || true) \
      && rm -rf /var/lib/apt/lists/*; \
    fi

COPY --from=builder /tvproxy /usr/local/bin/tvproxy
COPY --from=builder /usr/local/lib/libav*.so* /usr/local/lib/
COPY --from=builder /usr/local/lib/libsw*.so* /usr/local/lib/
RUN ldconfig
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
ENV TVPROXY_DB_PATH=/config/tvproxy.db
ENV TVPROXY_RECORD_DIR=/record
ENV TVPROXY_VOD_OUTPUT_DIR=/record

# Hardware acceleration:
#   Intel VAAPI/QSV: mount /dev/dri, set LIBVA_DRIVER_NAME=iHD (default below)
#   NVIDIA NVENC/NVDEC: run with --gpus all (no extra packages needed)
#   AMD VAAPI: mount /dev/dri, set LIBVA_DRIVER_NAME=radeonsi
ENV LIBVA_DRIVER_NAME=iHD

EXPOSE 8080

ENTRYPOINT ["entrypoint.sh"]
