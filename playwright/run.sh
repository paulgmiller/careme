#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PORT:-18080}"
HOST_APP_URL="http://127.0.0.1:${PORT}"
CONTAINER_APP_URL="${APP_URL:-http://127.0.0.1:${PORT}}"
SERVER_LOG="${SERVER_LOG:-/tmp/careme-playwright-server.log}"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "Starting Careme at ${HOST_APP_URL} (ENABLE_MOCKS=1)..."
(
  cd "${ROOT_DIR}"
  ENABLE_MOCKS=1 \
  GOCACHE=/tmp/go-build \
  GOMODCACHE=/tmp/go-modcache \
  go run ./cmd/careme -serve -addr ":${PORT}" >"${SERVER_LOG}" 2>&1
) &
SERVER_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS "${HOST_APP_URL}/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl -fsS "${HOST_APP_URL}/ready" >/dev/null 2>&1; then
  echo "Server did not become ready. Last logs:"
  tail -n 80 "${SERVER_LOG}" || true
  exit 1
fi

docker run --rm \
  --network host \
  -u "$(id -u):$(id -g)" \
  -v "${ROOT_DIR}:/work" \
  -w /work/playwright \
  -e APP_URL="${CONTAINER_APP_URL}" \
  mcr.microsoft.com/playwright:v1.51.1-noble \
  bash -lc "npm install --no-audit --no-fund && npx playwright test"
