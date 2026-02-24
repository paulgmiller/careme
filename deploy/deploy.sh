#!/usr/bin/env bash
set -euo pipefail

ref="${1:-origin/master}"
deploy_file="deploy/deploy.yaml"
namespace="${2:-careme}"
short_len=7

if ! command -v envsubst >/dev/null 2>&1; then
  echo "error: envsubst is required but not found in PATH" >&2
  exit 1
fi

git fetch

if ! command -v kubectl >/dev/null 2>&1; then
  echo "error: kubectl is required but not found in PATH" >&2
  exit 1
fi

if ! commit_hash="$(git rev-parse --verify "${ref}^{commit}" 2>/dev/null)"; then
  echo "error: could not resolve ref '${ref}' to a commit" >&2
  exit 1
fi

export IMAGE_TAG="${commit_hash:0:${short_len}}"

if [[ ! -f "${deploy_file}" ]]; then
  echo "error: deploy file not found: ${deploy_file}" >&2
  exit 1
fi

if [[ "$(<"${deploy_file}")" != *'${IMAGE_TAG}'* ]]; then
  echo "error: ${deploy_file} does not contain \${IMAGE_TAG}" >&2
  exit 1
fi

echo "Deploying image: ${IMAGE_TAG}"
envsubst '${IMAGE_TAG}' <"${deploy_file}" | kubectl apply -f - -n "${namespace}"
