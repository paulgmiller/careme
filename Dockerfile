# Multi-stage build for careme service
# Stage 1: build
FROM golang:1.26-alpine AS builder
WORKDIR /src
ARG CMD_PATH=./cmd/careme
# Go invokes Git to populate the standard vcs.* build settings. Git remains in
# this builder stage and is not copied into the final distroless image.
RUN apk add --no-cache git
# Enable module cache
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build static binary (no CGO)
# Fail the build if the Git metadata retained by BuildKit is unavailable rather
# than publishing a binary whose runtime build information is "unknown".
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -buildvcs=true \
    -ldflags="-s -w" \
    -o /out/app ${CMD_PATH}

# Stage 2: minimal runtime image
FROM gcr.io/distroless/static:nonroot
WORKDIR /workspace
COPY --from=builder /out/app /app
# Copy CA certs (distroless already has them, included for clarity)
# COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/app"]
