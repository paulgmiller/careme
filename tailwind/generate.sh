#!/usr/bin/env bash
set -euo pipefail

# Use a lockfile install and tracked content to ensure deterministic output.
npm --prefix tailwind ci --no-audit --no-fund

content_files=$(git ls-files "internal/templates/*.html" "internal/templates/**/*.html" | sort -u)
if [ -z "$content_files" ]; then
  echo "No tracked template files found under internal/templates." >&2
  exit 1
fi
content_arg=$(printf '%s' "$content_files" | paste -sd, -)

npx --prefix tailwind tailwindcss \
  -i ./tailwind/input.css \
  -o ./cmd/careme/static/tailwind.css \
  --minify \
  --content "$content_arg"

# Normalize output to include a trailing newline for stable diffs.
if [ -s ./cmd/careme/static/tailwind.css ] && [ "$(tail -c1 ./cmd/careme/static/tailwind.css)" != $'\n' ]; then
  printf '\n' >> ./cmd/careme/static/tailwind.css
fi
