# ---------- Stage 1: Build ----------
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install git (needed for go mod)
RUN apk add --no-cache git

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary & Let Go determine the architecture dynamically based on the host
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o tunnel-server ./cmd/server

# ---------- Stage 2: Runtime ----------
FROM alpine:latest

WORKDIR /app

# Add CA certs (important for HTTPS tunnels)
RUN apk --no-cache add ca-certificates

# Copy binary
COPY --from=builder /app/tunnel-server .

# Expose ports (example)
EXPOSE 8080

# Run
CMD ["./tunnel-server"]