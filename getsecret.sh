#!/usr/bin/env bash
set -euo pipefail

if [[ $# -gt 3 ]]; then
  echo "Usage: $0 <secret-name> [namespace] [output-file]"
  echo "  secret-name  - name of the Kubernetes Secret"
  echo "  namespace    - Kubernetes namespace (default: default)"
  echo "  output-file  - path to .env file (default: .env)"
  exit 1
fi

SECRET_NAME="${1:-careme-secrets}"
NAMESPACE="${2:-careme}"
OUTPUT_FILE="${3:-.env}"

# Fetch secret and convert to KEY=VALUE lines
kubectl get secret "${SECRET_NAME}" \
  -n "${NAMESPACE}" \
  -o json \
  | jq -r '
    .data
    | to_entries[]
    | "\(.key)=\(.value | @base64d)"
  ' > "${OUTPUT_FILE}"

echo "Wrote environment variables from secret '${SECRET_NAME}' (ns: ${NAMESPACE}) to ${OUTPUT_FILE}"
