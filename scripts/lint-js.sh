#!/usr/bin/env bash
set -euo pipefail

if ! command -v node >/dev/null 2>&1; then
  echo "node is required for JS lint checks" >&2
  exit 1
fi

if [ "$#" -gt 0 ]; then
  files=("$@")
else
  mapfile -t files < <(find cmd/careme/static -maxdepth 1 -type f -name '*.js' | sort)
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "No JavaScript files to lint."
  exit 0
fi

for file in "${files[@]}"; do
  node --check "$file"
done
