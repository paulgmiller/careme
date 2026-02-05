#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
image_name="careme-tailwind:latest"

docker build -f "$repo_root/tailwind/Dockerfile" -t "$image_name" "$repo_root"

content_files=$(git -C "$repo_root" ls-files "internal/templates/*.html" "internal/templates/**/*.html" | sort -u)
if [ -z "$content_files" ]; then
  echo "No tracked template files found under internal/templates." >&2
  exit 1
fi
content_arg=$(printf '%s' "$content_files" | paste -sd, -)

docker run --rm \
  -v "$repo_root":/work \
  -w /work \
  -e CONTENT_ARG="$content_arg" \
  "$image_name" \
  /bin/sh -c '/opt/tailwind/node_modules/.bin/tailwindcss \
    -i /work/tailwind/input.css \
    -o /work/cmd/careme/static/tailwind.css \
    --minify \
    --content "$CONTENT_ARG"'

# Normalize output to include a trailing newline for stable diffs.
if [ -s "$repo_root/cmd/careme/static/tailwind.css" ] && [ "$(tail -c1 "$repo_root/cmd/careme/static/tailwind.css")" != $'\n' ]; then
  printf '\n' >> "$repo_root/cmd/careme/static/tailwind.css"
fi
