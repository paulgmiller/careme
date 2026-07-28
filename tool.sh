#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export GOTOOLCHAIN=go1.26.2+auto
exec go tool -C "${repo_root}" -modfile=tools/tool.mod "$@"
