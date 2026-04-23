#!/usr/bin/env bash
set -euo pipefail

ref="${1:-origin/master}"
manifest_paths=(
  "deploy/deploy.yaml"
  "deploy/cronjob-careme-mail.yaml"
  "deploy/cronjob-albertsons-scrape.yaml"
  "deploy/cronjob-albertsons-reese84.yaml"
  "deploy/cronjob-wholefoods-scrape.yaml"
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

for manifest_path in "${manifest_paths[@]}"; do
  if ! git cat-file -e "${ref}:${manifest_path}" 2>/dev/null; then
    echo "error: deploy file not found in ref '${ref}': ${manifest_path}" >&2
    exit 1
  fi
done

echo "Deploying image: ${IMAGE_TAG}"
for manifest_path in "${manifest_paths[@]}"; do
  git show "${ref}:${manifest_path}" | envsubst '${IMAGE_TAG}' | kubectl apply -f - -n "${namespace}"
done

echo "Waiting for rollout of deployment/careme"
kubectl rollout status deployment/careme -n "${namespace}" -w
