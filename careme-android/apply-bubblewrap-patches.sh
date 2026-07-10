#!/usr/bin/env bash
set -euo pipefail

manifest="app/src/main/AndroidManifest.xml"
permission='        <uses-permission android:name="android.permission.ACCESS_COARSE_LOCATION"/>'

if grep -q 'android.permission.ACCESS_COARSE_LOCATION' "$manifest"; then
  exit 0
fi

tmp="$(mktemp)"
awk -v permission="$permission" '
  {
    print
    if ($0 ~ /android.permission.POST_NOTIFICATIONS/ && !added) {
      print permission
      added = 1
    }
  }
  END {
    if (!added) {
      exit 1
    }
  }
' "$manifest" > "$tmp"
mv "$tmp" "$manifest"
