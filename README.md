# gofsaas

gofsaas is a demand-driven, blocking FUSE filesystem for Amazon S3 with container-aware coordination. It presents an S3 bucket (or prefix) as a read-only local directory, fetching objects on first access and caching them locally.

## Features

- **Demand-driven**: files are fetched from S3 only when first opened, then served from a local disk cache
- **Single-flight deduplication**: concurrent requests for the same file share a single S3 fetch
- **Blocking open**: `open(2)` does not return until the file is fully downloaded and ready to read
- **Unix socket API**: a companion socket server exposes `exists`, `fetch`, `clean`, and `status` operations for container-to-container coordination
- **Interface-first, testable**: all dependencies are interfaces; fakes are provided for unit testing without AWS credentials

## Architecture

```
┌─────────────────────────────┐
│  gofsaas mount process      │
│  ┌──────────┐ ┌──────────┐  │
│  │  FUSE FS │ │  Socket  │  │
│  │  (fs.go) │ │  Server  │  │
│  └────┬─────┘ └────┬─────┘  │
│       │            │        │
│  ┌────▼────────────▼─────┐  │
│  │     Handler Core      │  │
│  │  resolver / s3client  │  │
│  │  state.StateMap       │  │
│  │  cache.DiskCache      │  │
│  └───────────────────────┘  │
└─────────────────────────────┘
```

## Prerequisites

- Go 1.24+
- Linux with FUSE support (`/dev/fuse` present)
- `libfuse3-dev` and `fuse3` packages
- AWS credentials (via environment, instance role, or `~/.aws/`)

Install FUSE development headers on Ubuntu/Debian:

```bash
sudo apt-get install libfuse3-dev fuse3
```

## Installation

```bash
git clone https://github.com/your-org/gofsaas
cd gofsaas
go build -o gofsaas ./cmd/gofsaas
sudo install -m 0755 gofsaas /usr/local/bin/gofsaas
```

Or use the install script:

```bash
./deploy/install.sh
```

## Usage

### Mount

Mount an S3 prefix as a local directory:

```bash
gofsaas mount \
  --mount /files \
  --bucket my-genomics-bucket \
  --prefix data/ \
  --cache-dir /tmp/gofsaas-cache \
  --socket /run/gofsaas/gofsaas.sock
```

Environment variable equivalents:

- `GOFSAAS_MOUNT` — mount point
- `GOFSAAS_BUCKET` — S3 bucket name
- `GOFSAAS_PREFIX` — S3 key prefix
- `GOFSAAS_CACHE_DIR` — local cache directory (default: `/tmp/gofsaas-cache`)
- `GOFSAAS_SOCKET` — Unix socket path (default: `/run/gofsaas/gofsaas.sock`)

### Socket API

Check if a file exists in S3:

```bash
gofsaas exists --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Pre-fetch a file into the local cache:

```bash
gofsaas fetch --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Evict a cached file:

```bash
gofsaas clean --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Get server status:

```bash
gofsaas status --socket /run/gofsaas/gofsaas.sock
```

### JSON Response Format

All socket operations return JSON:

```json
{
  "exists": true,
  "cached": false,
  "size_bytes": 12345678,
  "ok": true,
  "duration_ms": 340,
  "freed_bytes": 12345678
}
```

## Running Tests

```bash
go test ./...
```

With the race detector:

```bash
go test -race ./...
```

## Package Overview

| Package | Description |
|---------|-------------|
| `pkg/resolver` | Converts absolute mount paths to S3 bucket/key pairs |
| `pkg/s3client` | S3 client interface + AWS SDK v2 implementation |
| `pkg/state` | Single-flight fetch state machine (concurrent-safe) |
| `pkg/cache` | Atomic disk cache with reference counting |
| `pkg/socket` | Unix socket server and JSON protocol |
| `pkg/fuse` | FUSE filesystem (demand-fetching read-only FS) |
| `pkg/gofsaas` | Client library for the socket API |
| `pkg/fakes` | In-memory fakes for testing without AWS or FUSE |

## ECS Deployment

See `deploy/ecs-task-snippet.json` for an example ECS task definition that runs gofsaas as a sidecar container. The sidecar requires:

- `SYS_ADMIN` capability
- Access to `/dev/fuse`
- A shared volume for the mount point

## License

MIT