#!/usr/bin/env bash
# install.sh — builds and installs the gofsaas binary on a Linux host.
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SOCKET_DIR="${SOCKET_DIR:-/run/gofsaas}"

# Require Go 1.22+
if ! command -v go &>/dev/null; then
    echo "Error: go is not installed or not in PATH" >&2
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED="1.22"
if [[ "$(printf '%s\n' "$REQUIRED" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED" ]]; then
    echo "Error: Go $REQUIRED or higher required (found $GO_VERSION)" >&2
    exit 1
fi

# Require fuse3 headers
if ! pkg-config --exists fuse3 2>/dev/null; then
    echo "Warning: fuse3 not found via pkg-config. Install libfuse3-dev." >&2
fi

echo "Building gofsaas..."
cd "$(dirname "$0")/.."
go build -o gofsaas ./cmd/gofsaas

echo "Installing to $INSTALL_DIR..."
install -m 0755 gofsaas "$INSTALL_DIR/gofsaas"
rm -f gofsaas

echo "Creating socket directory $SOCKET_DIR..."
mkdir -p "$SOCKET_DIR"
chmod 0755 "$SOCKET_DIR"

echo "Done. Run 'gofsaas --help' to get started."
