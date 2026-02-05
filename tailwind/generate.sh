#!/usr/bin/env bash
set -euo pipefail

# Use a Docker image so local npm/node versions are irrelevant.
docker build -f tailwind/Dockerfile -t careme-tailwind:local .

docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace/tailwind \
  careme-tailwind:local \
  sh -c '"$TAILWIND_BIN" -i ./input.css -o ../cmd/careme/static/tailwind.css --minify'
