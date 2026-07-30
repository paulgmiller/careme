# Multi-stage build for careme service
# Stage 1: build
FROM golang:1.26-alpine AS builder
WORKDIR /src
ARG CMD_PATH=./cmd/careme
# Enable module cache
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build static binary (no CGO)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/app ${CMD_PATH}

# Stage 2: minimal runtime image
FROM gcr.io/distroless/static:nonroot
WORKDIR /workspace
COPY --from=builder /out/app /app
# Copy CA certs (distroless already has them, included for clarity)
# COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app"]
