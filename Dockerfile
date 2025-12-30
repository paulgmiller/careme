# Multi-stage build for careme service
# Stage 1: Build CSS
FROM node:20-alpine AS css-builder
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY static/input.css ./static/
COPY tailwind.config.js ./
COPY internal/templates ./internal/templates/
RUN npm run build:css

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS go-builder
WORKDIR /src
# Enable module cache
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./... -count=1
# Build static binary (no CGO)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o careme ./cmd/careme

# Stage 3: minimal runtime image
FROM gcr.io/distroless/static:nonroot
WORKDIR /workspace
COPY --from=go-builder /src/careme /careme
COPY --from=css-builder /src/static/output.css /workspace/static/output.css
COPY --from=go-builder /src/internal/templates /workspace/internal/templates
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/careme"]
CMD ["-serve"]
