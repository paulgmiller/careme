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

public_origin="${PUBLIC_ORIGIN:-}"
ingress_rules="${INGRESS_RULES:-}"

if [[ -z "${public_origin}" || -z "${ingress_rules}" ]]; then
  case "${namespace}" in
    careme)
      public_origin="${public_origin:-https://careme.cooking}"
      ingress_rules="${ingress_rules:-$(cat <<'EOF'
    - host: careme.northbriton.net
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: careme
                port:
                  number: 80
    - host: careme.cooking
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: careme
                port:
                  number: 80
EOF
)}"
      ;;
    caremetest)
      public_origin="${public_origin:-https://test.careme.cooking}"
      ingress_rules="${ingress_rules:-$(cat <<'EOF'
    - host: test.careme.cooking
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: careme
                port:
                  number: 80
EOF
)}"
      ;;
    *)
      if [[ -z "${public_origin}" || -z "${ingress_rules}" ]]; then
        echo "error: PUBLIC_ORIGIN and INGRESS_RULES must be set for namespace '${namespace}'" >&2
        exit 1
      fi
      ;;
  esac
fi

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
export PUBLIC_ORIGIN="${public_origin}"
export INGRESS_RULES="${ingress_rules}"

for manifest_path in "${manifest_paths[@]}"; do
  if ! git cat-file -e "${ref}:${manifest_path}" 2>/dev/null; then
    echo "error: deploy file not found in ref '${ref}': ${manifest_path}" >&2
    exit 1
  fi
done

if [[ "${namespace}" == "caremetest" ]]; then
  if ! git show "${ref}:deploy/deploy.yaml" | grep -q '\${INGRESS_RULES}'; then
    echo "error: ref '${ref}' does not contain namespace-aware ingress rendering" >&2
    echo "hint: deploy a ref that includes the deploy/deploy.yaml placeholder changes" >&2
    exit 1
  fi
fi

echo "Deploying image: ${IMAGE_TAG}"
echo "Deploying namespace: ${namespace}"
echo "Using public origin: ${PUBLIC_ORIGIN}"
for manifest_path in "${manifest_paths[@]}"; do
  git show "${ref}:${manifest_path}" | envsubst '${IMAGE_TAG} ${PUBLIC_ORIGIN} ${INGRESS_RULES}' | kubectl apply -f - -n "${namespace}"
done

echo "Waiting for rollout of deployment/careme"
kubectl rollout status deployment/careme -n "${namespace}" -w
