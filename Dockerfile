# Multi-stage Dockerfile for Go application
# Stage 1: Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk --no-cache add ca-certificates git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Generate protobuf files (skip if proto files don't exist)
RUN apk --no-cache add protobuf protobuf-dev
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# Skip protoc generation in Docker build - proto files should be pre-generated

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o inventory-api \
    ./cmd/inventory-api

# Stage 2: Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/inventory-api .

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose ports
EXPOSE 8020 8021

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8021/health || exit 1

# Command to run
CMD ["./inventory-api"]