#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

<<<<<<< HEAD
export GOTOOLCHAIN=go1.26.2+auto
=======
>>>>>>> origin/master
exec go tool -C "${repo_root}" -modfile=tools/tool.mod "$@"
