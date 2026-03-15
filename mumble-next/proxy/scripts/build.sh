#!/bin/bash
# Build script for Mumble WebSocket Proxy

set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
COMMIT=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}
BUILD_DATE=${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}

LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}"

echo "Building Mumble WebSocket Proxy..."
echo "  Version:    ${VERSION}"
echo "  Commit:     ${COMMIT}"
echo "  Build Date: ${BUILD_DATE}"
echo ""

# Create bin directory
mkdir -p bin

# Build for current platform
echo "Building for current platform..."
go build -ldflags "${LDFLAGS}" -o bin/mumble-proxy ./cmd/mumble-proxy

echo ""
echo "Build complete: bin/mumble-proxy"