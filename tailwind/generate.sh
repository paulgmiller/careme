#!/usr/bin/env bash
set -euo pipefail

# Use a lockfile install and explicit content to ensure deterministic output.
npm --prefix tailwind ci --no-audit --no-fund
npx --prefix tailwind tailwindcss \
  -i ./tailwind/input.css \
  -o ./cmd/careme/static/tailwind.css \
  --minify \
  --content "./internal/templates/**/*.html"

# Normalize output to include a trailing newline for stable diffs.
if [ -s ./cmd/careme/static/tailwind.css ] && [ "$(tail -c1 ./cmd/careme/static/tailwind.css)" != $'\n' ]; then
  printf '\n' >> ./cmd/careme/static/tailwind.css
fi
