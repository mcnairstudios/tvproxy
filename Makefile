BINARY    := tvproxy
IMAGE     := gavinmcnair/tvproxy
TAG       := latest
BUILDER   := mybuilder
PLATFORMS := linux/amd64,linux/arm64
VERSION   := $(shell git rev-parse --short HEAD)$(shell git diff --quiet || echo -dirty-$(shell date +%s))

.PHONY: build test docker-build docker-push clean

## Local build
build:
	go build -ldflags="-s -w -X main.buildVersion=$(VERSION)" -o $(BINARY) ./cmd/tvproxy/

## Run all tests
test:
	go test ./...

## Build multi-arch Docker image and push to Docker Hub
docker-build:
	docker buildx build --builder $(BUILDER) --platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(TAG) --push .

## Build local Docker image only (current arch, no push)
docker-local:
	docker compose build

## Run locally via docker compose
run:
	docker compose up -d

## Tail container logs
logs:
	docker compose logs -f

## Clean build artifacts
clean:
	rm -f $(BINARY)
