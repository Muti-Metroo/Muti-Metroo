# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files first for caching
COPY go.mod go.sum* ./
RUN go mod download || true

# Copy source code
COPY . .

# Ensure all dependencies are downloaded (including newly added ones)
RUN go mod download

# Build the binary with version info
RUN VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev") && \
    BUILD_TIME=$(date -u +%Y-%m-%d_%H:%M:%S) && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME" -o /muti-metroo ./cmd/muti-metroo

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /muti-metroo /usr/local/bin/muti-metroo

# Create data directory
RUN mkdir -p /app/data /app/certs

# Default config location
VOLUME ["/app/data", "/app/certs"]

EXPOSE 4433/udp 8443/tcp 1080/tcp

ENTRYPOINT ["muti-metroo"]
CMD ["run", "-c", "/app/config.yaml"]
