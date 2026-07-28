#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tool_dir="${repo_root}/bin"

# shellcheck source=/dev/null
source "${repo_root}/tools/versions.sh"

mkdir -p "${tool_dir}"
curl -sSfL https://golangci-lint.run/install.sh |
  sh -s -- -b "${tool_dir}" "${GOLANGCI_LINT_VERSION}"
