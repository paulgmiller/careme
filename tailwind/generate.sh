#!/usr/bin/env bash
set -euo pipefail

# Use a Docker image so local npm/node versions are irrelevant.
docker build -f tailwind/Dockerfile -t careme-tailwind:local .

content_files=$(git ls-files "internal/templates/*.html" "internal/templates/**/*.html" | sort -u)
if [ -z "$content_files" ]; then
  echo "No tracked template files found under internal/templates." >&2
  exit 1
fi
content_arg=$(printf '%s' "$content_files" | paste -sd, -)

docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  -e CONTENT_ARG="$content_arg" \
  careme-tailwind:local \
  sh -c '"$TAILWIND_BIN" -i ./tailwind/input.css -o ./cmd/careme/static/tailwind.css --minify --content "$CONTENT_ARG"'

# Normalize output to include a trailing newline for stable diffs.
if [ -s ./cmd/careme/static/tailwind.css ] && [ "$(tail -c1 ./cmd/careme/static/tailwind.css)" != $'\n' ]; then
  printf '\n' >> ./cmd/careme/static/tailwind.css
fi
