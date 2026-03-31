#!/usr/bin/env bash
set -euo pipefail

ref="${1:-origin/master}"
deploy_dir="deploy"
manifest_files=(
  "${deploy_dir}/deploy.yaml"
  "${deploy_dir}/cronjob-careme-mail.yaml"
  "${deploy_dir}/cronjob-albertsons-reese84.yaml"
  "${deploy_dir}/cronjob-wholefoods-scrape.yaml"
)
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

for manifest_file in "${manifest_files[@]}"; do
  if [[ ! -f "${manifest_file}" ]]; then
    echo "error: deploy file not found: ${manifest_file}" >&2
    exit 1
  fi
done

echo "Deploying image: ${IMAGE_TAG}"
for manifest_file in "${manifest_files[@]}"; do
  envsubst '${IMAGE_TAG}' <"${manifest_file}" | kubectl apply -f - -n "${namespace}"
done

echo "Waiting for rollout of deployment/careme"
kubectl rollout status deployment/careme -n "${namespace}" -w
