# gofsaas

gofsaas is a demand-driven, blocking FUSE filesystem for Amazon S3 with container-aware coordination. It presents an S3 bucket (or prefix) as a read-only local directory, supporting two complementary access modes that share a single `StateMap` for automatic interoperation.

## Features

- **Dual-mode access**: transparent FUSE blocking and explicit socket-based prefetch, sharing the same state
- **Transparent mode (FUSE)**: `open(2)` checks the `StateMap` — returns immediately if cached, blocks on an in-flight download, or triggers a new S3 fetch — no application changes required
- **Explicit prefetch mode (socket)**: `fetch` fires off an async download and returns immediately; a subsequent `open(2)` blocks only on the remaining download time
- **Single-flight deduplication**: whether triggered by FUSE `Open` or socket `fetch`, concurrent requests for the same file share a single S3 download via the same `StateMap` entry and done channel
- **Blocking open**: `open(2)` does not return until the file is fully downloaded and ready to read, regardless of which mode initiated the download
- **Unix socket API**: a companion socket server exposes `exists`, `fetch` (with optional `--wait`), `clean`, and `status` operations for container-to-container coordination
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

### Access Modes

gofsaas supports two access modes that interoperate transparently through a shared `StateMap`:

#### Mode 1 — Transparent (FUSE)

Applications read files normally via `open(2)`. The FUSE `Open` handler checks the `StateMap`:

| StateMap entry | FUSE `Open` behavior |
|----------------|----------------------|
| `StateCached` | Returns fd immediately (cache hit) |
| `StateFetching` | Blocks on the existing done channel until download completes |
| `StateUnknown` | Triggers a new S3 download, sets `StateFetching`, blocks until complete |

No changes are needed in the calling container — it just does `open()` normally.

#### Mode 2 — Explicit prefetch (socket `fetch`)

A coordination container calls `fetch` via the Unix socket to start the download ahead of time. `fetch` is **non-blocking** — it spawns the download goroutine and returns immediately. If the data container later calls `open()` while the download is still in progress, FUSE `Open` sees `StateFetching` and blocks on the existing done channel — no duplicate S3 call.

**Key invariant:** both modes write through the same `StateMap` entry. Whether a download was triggered by `Open` or `fetch`, it uses the same `StateFetching` done channel. This is guaranteed by the single-flight design.

### Socket API

Check if a file exists in S3:

```bash
gofsaas exists --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Pre-fetch a file (non-blocking, returns immediately):

```bash
gofsaas fetch --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Pre-fetch a file (blocking, waits for download to complete):

```bash
gofsaas fetch --wait --socket /run/gofsaas/gofsaas.sock /files/samples/HG001.bam
```

Evict a cached file (resets `StateMap` to `StateUnknown` and deletes from disk):

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
| `pkg/fuse` | FUSE filesystem with full state-check-and-download logic in `Open` handler |
| `pkg/gofsaas` | Client library for the socket API (`Fetch`, `FetchWait`, `Clean`, `Exists`, `Status`) |
| `pkg/fakes` | In-memory fakes for testing without AWS or FUSE |

## ECS Deployment

See `deploy/ecs-task-snippet.json` for an example ECS task definition that runs gofsaas as a sidecar container. The sidecar requires:

- `SYS_ADMIN` capability
- Access to `/dev/fuse`
- A shared volume for the mount point

## License

MIT