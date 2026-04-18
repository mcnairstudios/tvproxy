# CI Build Pipeline

## Why

The current Docker builds use QEMU emulation for cross-architecture builds, taking 45-60+ minutes per architecture. The new pipeline builds natively on dedicated runners — both architectures build in parallel with no emulation.

## Build chain

```
gavinmcnair/gstreamer:1.4 (Docker Hub — base image, built by gstreamer-plugin repo)
    ↓ used as FROM in this repo's Dockerfile
mcnairstudios/tvproxy (this repo — Go app + final image)
    ↓ pushed to Docker Hub
gavinmcnair/tvproxy:latest (Docker Hub — multi-arch)
```

The gstreamer base image must be built first. This repo builds on top of it.

## CI runners (live and validated)

Two self-hosted GitHub Actions runners are registered to the `mcnairstudios` GitHub org. Both are online and proven working.

| Runner | Host | Architecture |
|--------|------|-------------|
| amd64 | TrueNAS Xeon 64-core, 192GB | linux/amd64 |
| arm64 | Mac Studio M3 Max, 64GB | linux/arm64 |

Each runner builds for its own architecture only. No QEMU. Both jobs run in parallel.

## Critical rules for workflows

### Docker Hub login is mandatory before every push

There are NO GitHub Actions secrets. The runner containers have Docker Hub credentials in their environment. Every job that does `docker push` MUST include this step first:

```yaml
- name: Log in to Docker Hub
  run: echo "${DOCKERHUB_PASSWORD}" | docker login -u "${DOCKERHUB_LOGIN}" --password-stdin
```

Without this, `docker push` fails with "access denied".

### Use `${{ env.VAR }}` not `$VAR`

Workflow-level `env:` vars must be referenced as `${{ env.IMAGE }}`, not bare `$IMAGE`.

## What to do in this repo

### 1. Add the build workflow

Create `.github/workflows/build.yml`:

```yaml
name: Build tvproxy

on:
  push:
    branches: [main]
  workflow_dispatch:

env:
  IMAGE: gavinmcnair/tvproxy
  TAG: latest

jobs:
  build-amd64:
    runs-on: [self-hosted, linux, amd64]
    steps:
      - uses: actions/checkout@v4
      - name: Log in to Docker Hub
        run: echo "${DOCKERHUB_PASSWORD}" | docker login -u "${DOCKERHUB_LOGIN}" --password-stdin
      - name: Build
        run: docker build -t ${{ env.IMAGE }}:${{ env.TAG }}-amd64 .
      - name: Push
        run: docker push ${{ env.IMAGE }}:${{ env.TAG }}-amd64

  build-arm64:
    runs-on: [self-hosted, linux, arm64]
    steps:
      - uses: actions/checkout@v4
      - name: Log in to Docker Hub
        run: echo "${DOCKERHUB_PASSWORD}" | docker login -u "${DOCKERHUB_LOGIN}" --password-stdin
      - name: Build
        run: docker build -t ${{ env.IMAGE }}:${{ env.TAG }}-arm64 .
      - name: Push
        run: docker push ${{ env.IMAGE }}:${{ env.TAG }}-arm64

  manifest:
    needs: [build-amd64, build-arm64]
    runs-on: [self-hosted, linux, arm64]
    steps:
      - name: Log in to Docker Hub
        run: echo "${DOCKERHUB_PASSWORD}" | docker login -u "${DOCKERHUB_LOGIN}" --password-stdin
      - name: Create and push multi-arch manifest
        run: |
          docker manifest create ${{ env.IMAGE }}:${{ env.TAG }} \
            --amend ${{ env.IMAGE }}:${{ env.TAG }}-amd64 \
            --amend ${{ env.IMAGE }}:${{ env.TAG }}-arm64
          docker manifest push ${{ env.IMAGE }}:${{ env.TAG }}

  verify:
    needs: [manifest]
    strategy:
      matrix:
        runner: [[self-hosted, linux, amd64], [self-hosted, linux, arm64]]
    runs-on: ${{ matrix.runner }}
    steps:
      - name: Pull and verify
        run: |
          docker pull ${{ env.IMAGE }}:${{ env.TAG }}
          docker run --rm ${{ env.IMAGE }}:${{ env.TAG }} tvproxy --version
```

### 2. Add a Makefile target for the runner build

Add a `make runner-build` target that replicates what the CI workflow does, for local testing:

```makefile
runner-build:
	docker build -t $(IMAGE):$(TAG)-$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') .
	docker push $(IMAGE):$(TAG)-$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
```

Keep the existing local build targets as-is — `make runner-build` is a separate path for the CI runners.

### 3. Update the Dockerfile base image tag

The current Dockerfile uses `gavinmcnair/gstreamer:1.2`. Once the gstreamer-plugin pipeline is producing `1.4`, update the FROM line:

```dockerfile
FROM gavinmcnair/gstreamer:1.4
```
