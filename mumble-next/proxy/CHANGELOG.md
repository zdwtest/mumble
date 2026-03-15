# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2024-01-15

### Added
- Commercial-grade WebSocket to TCP proxy
- Multi-tenant support with token-based routing
- Prometheus metrics integration
- Health check endpoints (`/health`, `/healthz`, `/readyz`)
- Connection statistics endpoint (`/stats`)
- YAML configuration file support
- Environment variable configuration
- Docker and Kubernetes deployment manifests
- Graceful shutdown support
- Connection rate limiting per IP
- Structured logging (JSON format)
- TLS support for secure connections
- Comprehensive unit test coverage
- E2E integration tests
- CI/CD pipeline with GitHub Actions
- OpenAPI documentation

### Changed
- Refactored from single-file implementation to modular architecture
- Improved connection handling with proper cleanup
- Enhanced error handling and logging

### Technical Details

#### Project Structure
```
proxy/
├── cmd/mumble-proxy/     # Application entry point
├── internal/
│   ├── proxy/            # Core proxy logic
│   ├── config/           # Configuration management
│   ├── metrics/          # Prometheus metrics
│   ├── health/           # Health checks
│   └── auth/             # Token authentication
├── test/e2e/             # E2E tests
├── deployments/          # Docker & Kubernetes
├── configs/              # Configuration files
└── api/                  # OpenAPI spec
```

#### Key Features
- **Token Authentication**: Route clients to different backends based on tokens
- **Rate Limiting**: Protect against connection floods
- **Metrics**: Monitor connection counts, duration, errors
- **Health Checks**: Kubernetes-ready probes

## [0.1.0] - 2023-12-01

### Added
- Initial minimal WebSocket proxy (~220 lines)
- Basic CLI arguments
- Simple logging
- WebSocket endpoint at `/mumble`
- Static file serving

---

## Upgrade Guide

### From 0.1.0 to 1.0.0

1. Update configuration:
   ```bash
   # Old: command line arguments
   ./mumble-proxy --murmur localhost --port 64738

   # New: YAML configuration file
   ./mumble-proxy --config configs/config.yaml
   ```

2. Update Docker deployment:
   ```bash
   # Pull new image
   docker pull mumble-proxy:1.0.0

   # Update environment variables
   docker run -e MUMBLE_MURMUR_HOST=mumble.example.com mumble-proxy:1.0.0
   ```

3. New endpoints:
   - `/health` - Detailed health check with Murmur status
   - `/healthz` - Kubernetes liveness probe
   - `/readyz` - Kubernetes readiness probe
   - `/metrics` - Prometheus metrics (port 9090)