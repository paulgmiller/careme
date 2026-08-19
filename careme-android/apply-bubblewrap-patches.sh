#!/usr/bin/env bash
set -euo pipefail

build_file="app/build.gradle"

if grep -q 'targetSdkVersion 36' "$build_file"; then
  exit 0
fi

tmp="$(mktemp)"
sed 's/targetSdkVersion 35/targetSdkVersion 36/' "$build_file" > "$tmp"

if ! grep -q 'targetSdkVersion 36' "$tmp"; then
  rm "$tmp"
  echo "could not update targetSdkVersion to 36" >&2
  exit 1
fi

mv "$tmp" "$build_file"
