# Mumble WebSocket Proxy (Commercial Edition)
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mumble-proxy ./cmd/mumble-proxy

# Final image
FROM alpine:3.19

WORKDIR /app

# Install necessary packages
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /mumble-proxy /app/mumble-proxy

# Copy default config and static files
COPY configs/config.yaml /app/configs/config.yaml
COPY web/ /app/web/

# Create non-root user
RUN addgroup -g 1000 mumble && \
    adduser -u 1000 -G mumble -s /bin/sh -D mumble && \
    chown -R mumble:mumble /app

USER mumble

# Expose ports
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

# Environment variables
ENV MUMBLE_SERVER_HOST=0.0.0.0 \
    MUMBLE_SERVER_PORT=8080 \
    MUMBLE_MURMUR_HOST=localhost \
    MUMBLE_MURMUR_PORT=64738 \
    MUMBLE_METRICS_ENABLED=true \
    MUMBLE_METRICS_PORT=9090

# Start
ENTRYPOINT ["/app/mumble-proxy"]
CMD ["--config", "/app/configs/config.yaml"]