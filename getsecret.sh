#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./getsecret.sh get [secrets-dir] [namespace]
  ./getsecret.sh put [secrets-dir] [namespace]

Commands:
  get          Fetch each Kubernetes Secret named by a *.env file in secrets-dir
               and write it back to that file.
  put          Apply each *.env file in secrets-dir to the Kubernetes Secret with
               the same basename.

Arguments:
  secrets-dir  Directory containing split env files (default: secrets/prod)
  namespace    Kubernetes namespace (default: careme)

Examples:
  ./getsecret.sh get
  ./getsecret.sh put secrets/prod
  ./getsecret.sh get secrets/test careme
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

sync_get() {
  local secrets_dir="$1"
  local namespace="$2"
  local files=()

  mkdir -p "${secrets_dir}"
  shopt -s nullglob
  files=("${secrets_dir}"/*.env)
  shopt -u nullglob

  if [[ ${#files[@]} -eq 0 ]]; then
    echo "error: no .env files found in ${secrets_dir}" >&2
    exit 1
  fi

  local file
  for file in "${files[@]}"; do
    local secret_name
    local tmp_file
    secret_name="$(basename "${file}" .env)"
    tmp_file="$(mktemp)"

    kubectl get secret "${secret_name}" \
      -n "${namespace}" \
      -o json \
      | jq -r '
        .data
        | to_entries
        | sort_by(.key)[]
        | "\(.key)=\(.value | @base64d)"
      ' > "${tmp_file}"

    mv "${tmp_file}" "${file}"
    echo "synced secret '${secret_name}' from namespace '${namespace}' to ${file}"
  done
}

sync_put() {
  local secrets_dir="$1"
  local namespace="$2"
  local files=()

  shopt -s nullglob
  files=("${secrets_dir}"/*.env)
  shopt -u nullglob

  if [[ ${#files[@]} -eq 0 ]]; then
    echo "error: no .env files found in ${secrets_dir}" >&2
    exit 1
  fi

  local file
  for file in "${files[@]}"; do
    local secret_name
    secret_name="$(basename "${file}" .env)"

    kubectl create secret generic "${secret_name}" \
      --from-env-file="${file}" \
      --dry-run=client \
      -o yaml \
      | kubectl apply -n "${namespace}" -f -

    echo "applied ${file} to secret '${secret_name}' in namespace '${namespace}'"
  done
}

main() {
  if [[ $# -lt 1 || $# -gt 3 ]]; then
    usage
    exit 1
  fi

  require_command kubectl
  require_command jq

  local command="$1"
  local secrets_dir="${2:-secrets/prod}"
  local namespace="${3:-careme}"

  case "${command}" in
    get)
      sync_get "${secrets_dir}" "${namespace}"
      ;;
    put)
      sync_put "${secrets_dir}" "${namespace}"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      echo "error: unknown command '${command}'" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
