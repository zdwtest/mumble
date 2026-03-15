#!/bin/bash
# Test script for Mumble WebSocket Proxy

set -e

echo "Running tests..."
echo ""

# Run unit tests
echo "=== Unit Tests ==="
go test -v -race ./internal/...

echo ""
echo "=== E2E Tests ==="
go test -v -race ./test/e2e/...

echo ""
echo "=== Test Coverage ==="
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1

echo ""
echo "Tests complete!"