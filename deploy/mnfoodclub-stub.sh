#!/usr/bin/env bash
set -euo pipefail

ref="${1:-origin/master}"
stub_namespace="mnfoodclub-stub"
commit_hash="$(git rev-parse --verify "${ref}^{commit}")"
export IMAGE_TAG="${commit_hash:0:7}"

# Namespace provisioning creates its CockroachDB database and roachkey secret.
git show "${ref}:deploy/mnfoodclub-stub-namespace.yaml" | kubectl apply -f -
kubectl wait --for=create secret/roachkey -n "${stub_namespace}" --timeout=120s

git show "${ref}:deploy/mnfoodclub-stub.yaml" | envsubst '${IMAGE_TAG}' | kubectl apply -f - -n "${stub_namespace}"
kubectl rollout status deployment/mnfoodclub-stub -n "${stub_namespace}" --timeout=180s

# Cut over only after the persistent service is ready. Existing caremetest URLs
# continue to work through the ingress in the new namespace.
kubectl delete ingress/mnfoodclub-stub deployment/mnfoodclub-stub service/mnfoodclub-stub -n caremetest --ignore-not-found
