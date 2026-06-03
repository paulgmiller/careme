#!/usr/bin/env bash
set -euo pipefail

ref="${1:-origin/master}"
app_manifest_path="deploy/deploy.yaml"
mail_manifest_path="deploy/cronjob-careme-mail.yaml"
cron_manifest_paths=(
  "deploy/cronjob-aldi-scrape.yaml"
  "deploy/cronjob-albertsons-scrape.yaml"
  "deploy/cronjob-albertsons-reese84.yaml"
  "deploy/cronjob-publix-scrape.yaml"
  "deploy/cronjob-publix-abck.yaml"
  "deploy/cronjob-wholefoods-scrape.yaml"
)
disabled_store_env=(
  "HEB_ENABLE=false"
  "WEGMANS_ENABLE=false"
)
namespace="${2:-careme}"
short_len=7

public_origin="${PUBLIC_ORIGIN:-}"
ingress_host="${INGRESS_HOST:-}"

if [[ -z "${ingress_host}" ]]; then
  case "${namespace}" in
    careme)
      ingress_host="${ingress_host:-careme.cooking}"
      ;;
    caremetest)
      ingress_host="${ingress_host:-test.careme.cooking}"
      ;;
    *)
      echo "error: INGRESS_HOST must be set for namespace '${namespace}'" >&2
      exit 1
      ;;
  esac
fi

public_origin="${public_origin:-https://${ingress_host}}"
manifest_paths=("${app_manifest_path}" "${mail_manifest_path}" "${cron_manifest_paths[@]}")
aldi_scrape_schedule="45 6 * * 0"
albertsons_scrape_schedule="0 6 * * 0"
albertsons_reese84_schedule="0 */6 * * *"
publix_scrape_schedule="30 6 * * 0"
publix_abck_schedule="15 */6 * * *"
wholefoods_scrape_schedule="0 6 * * 0"

if [[ "${namespace}" == "caremetest" ]]; then
  manifest_paths=("${app_manifest_path}" "${cron_manifest_paths[@]}")
  aldi_scrape_schedule="45 6 1,15 * *"
  albertsons_scrape_schedule="0 6 1,15 * *"
  albertsons_reese84_schedule="0 */12 * * *"
  publix_scrape_schedule="30 6 1,15 * *"
  publix_abck_schedule="15 6 * * *"
  wholefoods_scrape_schedule="0 6 1,15 * *"
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
export INGRESS_HOST="${ingress_host}"
export ALDI_SCRAPE_SCHEDULE="${aldi_scrape_schedule}"
export ALBERTSONS_SCRAPE_SCHEDULE="${albertsons_scrape_schedule}"
export ALBERTSONS_REESE84_SCHEDULE="${albertsons_reese84_schedule}"
export PUBLIX_SCRAPE_SCHEDULE="${publix_scrape_schedule}"
export PUBLIX_ABCK_SCHEDULE="${publix_abck_schedule}"
export WHOLEFOODS_SCRAPE_SCHEDULE="${wholefoods_scrape_schedule}"

for manifest_path in "${manifest_paths[@]}"; do
  if ! git cat-file -e "${ref}:${manifest_path}" 2>/dev/null; then
    echo "error: deploy file not found in ref '${ref}': ${manifest_path}" >&2
    exit 1
  fi
done

if [[ "${namespace}" == "caremetest" ]]; then
  if ! git show "${ref}:deploy/deploy.yaml" | grep -q '\${INGRESS_HOST}'; then
    echo "error: ref '${ref}' does not contain namespace-aware ingress rendering" >&2
    echo "hint: deploy a ref that includes the deploy/deploy.yaml ingress host placeholder change" >&2
    exit 1
  fi
  for cron_manifest_path in "${cron_manifest_paths[@]}"; do
    if ! git show "${ref}:${cron_manifest_path}" | grep -q '_SCHEDULE}'; then
      echo "error: ref '${ref}' does not contain schedule placeholders in ${cron_manifest_path}" >&2
      echo "hint: deploy a ref that includes the test CronJob schedule placeholder change" >&2
      exit 1
    fi
  done
fi

echo "Deploying image: ${IMAGE_TAG}"
echo "Deploying namespace: ${namespace}"
echo "Using public origin: ${PUBLIC_ORIGIN}"
echo "Using ingress host: ${INGRESS_HOST}"
for manifest_path in "${manifest_paths[@]}"; do
  git show "${ref}:${manifest_path}" | envsubst '${IMAGE_TAG} ${PUBLIC_ORIGIN} ${INGRESS_HOST} ${ALDI_SCRAPE_SCHEDULE} ${ALBERTSONS_SCRAPE_SCHEDULE} ${ALBERTSONS_REESE84_SCHEDULE} ${PUBLIX_SCRAPE_SCHEDULE} ${PUBLIX_ABCK_SCHEDULE} ${WHOLEFOODS_SCRAPE_SCHEDULE}' | kubectl apply -f - -n "${namespace}"
done

if [[ "${namespace}" == "caremetest" ]]; then
  echo "Disabling ALDI, and H-E-B integrations for caremetest deployment/careme"
  kubectl set env deployment/careme "${disabled_store_env[@]}" -n "${namespace}"
fi

echo "Waiting for rollout of deployment/careme"
kubectl rollout status deployment/careme -n "${namespace}" -w
