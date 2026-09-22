# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for version info and build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build the binary
RUN CGO_ENABLED=1 go build -o codergag ./cmd/codergag

# Runtime stage
FROM alpine:3.20

# Install CA certs and minimal runtime deps
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -S codergag && adduser -S -G codergag codergag

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/codergag .

# Copy example config
COPY --from=builder /app/codergag.yaml.example /app/codergag.yaml.example

# Create data directory
RUN mkdir -p /data && chown codergag:codergag /data

USER codergag

# Default config path
ENV CODERAG_CONFIG=/data/config.yaml
ENV CODERAG_MAINTENANCE_INTERVAL=15m

# Default database path
ENV CODERAG_DB_PATH=/data/codergag.db

EXPOSE 0

ENTRYPOINT ["./codergag"]
CMD ["serve"]