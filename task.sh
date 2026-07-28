#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

exec go tool -C "${repo_root}" -modfile=tools/task.mod task "$@"
