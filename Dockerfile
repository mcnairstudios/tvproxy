FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /tvproxy ./cmd/tvproxy/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /tvproxy /usr/local/bin/tvproxy

RUN adduser -D -u 1000 tvproxy
USER tvproxy
WORKDIR /data

EXPOSE 8080

ENTRYPOINT ["tvproxy"]
