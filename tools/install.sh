#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tool_dir="${repo_root}/bin"

# shellcheck source=/dev/null
source "${repo_root}/tools/versions.sh"

mkdir -p "${tool_dir}"
export GOBIN="${tool_dir}"

install_golangci_lint() {
  go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
}

install_gofumpt() {
  go install "mvdan.cc/gofumpt@${GOFUMPT_VERSION}"
}

install_kubeconform() {
  go install "github.com/yannh/kubeconform/cmd/kubeconform@${KUBECONFORM_VERSION}"
}

if [[ $# -eq 0 ]]; then
  set -- golangci-lint gofumpt kubeconform
fi

for tool_name in "$@"; do
  case "${tool_name}" in
    golangci-lint)
      install_golangci_lint
      ;;
    gofumpt)
      install_gofumpt
      ;;
    kubeconform)
      install_kubeconform
      ;;
    *)
      echo "unknown tool: ${tool_name}" >&2
      exit 1
      ;;
  esac
done
