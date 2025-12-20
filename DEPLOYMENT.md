# Deployment Documentation

This document describes the continuous deployment setup for the Careme application.

## Overview

The application uses GitHub Actions for continuous deployment with two separate workflows:

1. **Test Environment Deployment** - Automatically deploys every successful master commit to the `caremetest` namespace
2. **Production Deployment** - Deploys semantic version tags to the `careme` namespace

## Workflows

### 1. Deploy to Test Environment (`deploy-test.yaml`)

**Trigger:** Automatically runs after the "GHCR Publish & PR Gate" workflow completes successfully on the master branch.

**What it does:**
- Extracts the short commit SHA (7 characters) from the successful build
- Updates `deploy/deploy.yaml` with the new image tag `ghcr.io/paulgmiller/careme:<SHORT_SHA>`
- Deploys to the `caremetest` Kubernetes namespace
- Commits the updated manifest back to the repository

**Required Secrets:**
- `KUBECONFIG` - Base64-encoded Kubernetes configuration file with access to the `caremetest` namespace

### 2. Deploy to Production (`deploy-prod.yaml`)

**Trigger:** 
- Automatically on push of semantic version tags (e.g., `v1.0.0`, `v1.2.3`, `v2.0.0-beta.1`)
- Manually via workflow_dispatch with a tag name input

**What it does:**
- Determines the commit SHA for the tagged release
- Pulls the Docker image tagged with the commit SHA
- Retags the image with the semantic version tag
- Pushes the newly tagged image to GHCR
- Updates `deploy/deploy.yaml` with the semantic version tag
- Deploys to the `careme` Kubernetes namespace (production)
- Commits the updated manifest back to the repository

**Required Secrets:**
- `KUBECONFIG` - Base64-encoded Kubernetes configuration file with access to the `careme` namespace

## Setup Instructions

### 1. Configure Kubernetes Access

Generate a base64-encoded kubeconfig file:

```bash
# If you have a kubeconfig file
cat ~/.kube/config | base64 -w 0

# Or create a service account and get its token (recommended for production)
kubectl create serviceaccount github-deployer -n <namespace>
kubectl create rolebinding github-deployer-binding \
  --clusterrole=edit \
  --serviceaccount=<namespace>:github-deployer \
  -n <namespace>
```

### 2. Add Secrets to GitHub Repository

Go to your repository settings → Secrets and variables → Actions, and add:

- **KUBECONFIG**: The base64-encoded Kubernetes configuration file

The configuration should have permissions to:
- Apply deployments in both `careme` and `caremetest` namespaces
- Check rollout status

### 3. Verify Permissions

Ensure the GitHub Actions have the following permissions in `.github/workflows/`:
- `contents: write` - To commit updated deployment manifests
- `packages: read/write` - To pull and push Docker images to GHCR

## Deployment Flow

### Test Deployment Flow

```
Push to master → GHCR Publish & PR Gate → Build & Push Image
                                        ↓
                            Deploy to Test Environment
                                        ↓
                        Update deploy/deploy.yaml with SHA
                                        ↓
                      kubectl apply -n caremetest
                                        ↓
                      Commit updated manifest
```

### Production Deployment Flow

```
Create/Push Tag (v1.0.0) → Deploy to Production
                                 ↓
                    Find commit SHA for tag
                                 ↓
                    Pull image:<SHORT_SHA>
                                 ↓
                    Retag as image:v1.0.0
                                 ↓
                    Push retagged image
                                 ↓
            Update deploy/deploy.yaml with v1.0.0
                                 ↓
                kubectl apply -n careme
                                 ↓
                Commit updated manifest
```

## Creating a Release

To deploy to production:

1. Ensure all tests pass on master
2. Create and push a semantic version tag:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```
3. The deployment workflow will automatically:
   - Retag the corresponding Docker image
   - Deploy to the `careme` namespace
   - Update the deployment manifest

## Monitoring

After deployment, you can monitor the status:

```bash
# Check test environment
kubectl get pods -n caremetest
kubectl logs -n caremetest deployment/careme

# Check production environment
kubectl get pods -n careme
kubectl logs -n careme deployment/careme
```

## Rollback

To rollback a deployment:

```bash
# Rollback to previous version
kubectl rollout undo deployment/careme -n careme

# Or to a specific revision
kubectl rollout history deployment/careme -n careme
kubectl rollout undo deployment/careme -n careme --to-revision=<revision-number>
```

## Troubleshooting

### Deployment fails with authentication error
- Verify the `KUBECONFIG` secret is correctly base64-encoded
- Ensure the kubeconfig has valid credentials and hasn't expired
- Check that the service account has necessary RBAC permissions

### Image not found error
- Ensure the "GHCR Publish & PR Gate" workflow completed successfully before deployment
- Verify the image exists in GitHub Container Registry
- For production deploys, ensure the tag exists and matches a valid commit

### Manifest commit fails
- Verify the GitHub token has `contents: write` permission
- Check that the branch protection rules allow github-actions[bot] to push
