# Build stage
FROM golang:1.24-alpine AS builder

# Metadata labels
LABEL maintainer="Traffic Tacos Team" \
      description="Inventory API - High-performance gRPC service for ticket inventory management" \
      version="1.0.0"

# Install necessary packages for building
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user for security
ENV USER=appuser
ENV UID=10001

RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    "${USER}"

WORKDIR /build

# Copy dependency files first for better Docker layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the binary with optimizations
ARG TARGETARCH
RUN BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ") && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-arm64} go build \
    -ldflags="-w -s -extldflags \"-static\" -X main.version=1.0.0 -X main.buildTime=${BUILD_TIME}" \
    -trimpath \
    -tags netgo \
    -installsuffix cgo \
    -o inventory-api ./cmd/inventory-api \
    && chmod +x inventory-api

# Final stage - use distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12

# Copy runtime dependencies
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

# Copy timezone data if needed
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy our static executable
COPY --from=builder /build/inventory-api /inventory-api

# Use an unprivileged user
USER appuser:appuser

# Expose gRPC port
EXPOSE 8080

# Health check - check if binary is executable
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/inventory-api", "--help"] || exit 1

# Metadata
LABEL org.opencontainers.image.title="Inventory API" \
      org.opencontainers.image.description="High-performance gRPC service for ticket inventory management" \
      org.opencontainers.image.version="1.0.0" \
      org.opencontainers.image.vendor="Traffic Tacos"

# Run the binary
ENTRYPOINT ["/inventory-api"]
