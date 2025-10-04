# GitHub Actions CI/CD Setup

## Overview

This repository uses GitHub Actions for automated testing, building, and deployment with a GitOps workflow.

## Workflow Architecture

```
inventory-api (this repo)          deployment-repo
       │                                  │
       ├─ Code Push to main              │
       │                                  │
       ├─ Run Tests & Linter              │
       │  (Go tests, golangci-lint)      │
       │                                  │
       ├─ Build Docker Image              │
       │  (linux/amd64)                   │
       │                                  │
       ├─ Push to ECR                     │
       │  tag: {git-sha}                  │
       │  tag: latest                     │
       │                                  │
       ├─ Trivy Security Scan             │
       │                                  │
       └─ Trigger repository_dispatch ───►│
                                          │
                                          ├─ Update deployment.yaml
                                          │  (image tag)
                                          │
                                          ├─ Commit & Push
                                          │
                                          └─ ArgoCD Auto Sync
                                             (to EKS cluster)
```

## Workflow Files

### 1. `build.yml` (Main Workflow)
Comprehensive CI/CD pipeline with three jobs:

#### Job 1: Test & Lint
- Runs on: All pushes and PRs
- Actions:
  - Go dependency verification
  - Unit tests with race detector
  - Code coverage
  - golangci-lint
  
#### Job 2: Build & Push Docker Image
- Runs on: Push to main/develop (not PRs)
- Actions:
  - Docker image build (linux/amd64)
  - Push to Amazon ECR
  - Trivy security scan
  
#### Job 3: Update Deployment Manifest
- Runs on: Push to main only
- Actions:
  - Trigger deployment-repo via repository_dispatch
  - Update image tag in Kubernetes manifests

### 2. `release.yml` (Release Workflow)
- Runs on: Git tags (v*)
- Creates binary releases for multiple platforms
- Generates GitHub Release with artifacts

## Required Secrets

Configure the following secrets in this repository (Settings → Secrets and variables → Actions):

### 1. AWS_ROLE_ARN ⭐
AWS IAM Role ARN for OIDC authentication with GitHub Actions.

**Current Value:**
```
arn:aws:iam::137406935518:role/GitHubActionsECRRole
```

**Purpose:**
- Authenticate GitHub Actions to AWS without Access Keys
- Push Docker images to Amazon ECR
- Uses temporary credentials (OIDC method)

**Permissions:**
- `AmazonEC2ContainerRegistryPowerUser` policy attached

**Trust Policy:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::137406935518:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "token.actions.githubusercontent.com:sub": "repo:traffic-tacos/*:*"
        }
      }
    }
  ]
}
```

### 2. DEPLOYMENT_REPO_TOKEN ⭐
GitHub Personal Access Token (PAT) to trigger deployment-repo workflows.

**Purpose:**
- Send repository_dispatch events to deployment-repo
- Trigger automatic manifest updates

**Required Scopes:**
- `repo` (Full control of private repositories)

**How to Create:**
1. GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Generate new token (classic)
3. Name: `inventory-api-deployment-trigger`
4. Scopes: ✅ `repo`
5. Copy token and add to repository secrets

## Environment Variables

The workflow uses these environment variables (defined in workflow file):

| Variable | Value | Description |
|----------|-------|-------------|
| `AWS_REGION` | `ap-northeast-2` | AWS region for ECR |
| `ECR_REGISTRY` | `137406935518.dkr.ecr.ap-northeast-2.amazonaws.com` | ECR registry URL |
| `SERVICE_NAME` | `inventory-api` | Service name for tagging |
| `DEPLOYMENT_REPO` | `traffic-tacos/deployment-repo` | Target deployment repo |

## Image Tagging Strategy

- **Main branch:** `{git-short-sha}` (e.g., `a1b2c3d`)
- **Other branches:** `{branch-name}-{git-short-sha}` (e.g., `develop-a1b2c3d`)
- **Latest:** Always tagged on successful builds

Examples:
```bash
# Main branch commit abc1234
137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/traffic-tacos-inventory-api:abc1234
137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/traffic-tacos-inventory-api:latest

# Develop branch commit def5678
137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/traffic-tacos-inventory-api:develop-def5678
```

## Workflow Behavior

### On Pull Request:
1. ✅ Run tests
2. ✅ Run linter
3. ❌ Skip Docker build
4. ❌ Skip deployment

### On Push to Develop:
1. ✅ Run tests
2. ✅ Run linter
3. ✅ Build Docker image
4. ✅ Push to ECR (tag: develop-{sha}, latest)
5. ❌ Skip deployment trigger

### On Push to Main:
1. ✅ Run tests
2. ✅ Run linter
3. ✅ Build Docker image
4. ✅ Push to ECR (tag: {sha}, latest)
5. ✅ Trigger deployment-repo update
6. ✅ ArgoCD auto-sync to production

## Manual Workflow Dispatch

You can manually trigger the workflow:

```bash
# Using GitHub CLI
gh workflow run build.yml

# Or via GitHub UI: Actions → Build, Test and Deploy → Run workflow
```

## Troubleshooting

### Build fails with "exec format error"
- Ensure `TARGETARCH=amd64` is set in build-args
- Check Dockerfile has `GOARCH=${TARGETARCH:-amd64}`
- Verify EKS nodes are amd64 architecture

### ECR login fails
```
Error: failed to get ECR auth token
```
**Solution:**
- Verify `AWS_ROLE_ARN` secret is correctly set
- Check IAM role has ECR permissions
- Ensure OIDC provider exists in AWS IAM
- Verify trust policy allows this repository

### Deployment trigger fails
```
Error: repository_dispatch failed (401/403)
```
**Solution:**
- Verify `DEPLOYMENT_REPO_TOKEN` is valid
- Token must have `repo` scope
- Check deployment-repo has `repository_dispatch` workflow
- Confirm `DEPLOYMENT_REPO` variable is correct

### Tests fail
```
Error: make test: command not found
```
**Solution:**
- Tests run directly with `go test` if Makefile not found
- Ensure test files exist: `*_test.go`
- Check Go version compatibility (1.24)

### Linter warnings
- Linter continues on error (won't fail build)
- Fix warnings for better code quality
- Configure `.golangci.yml` for custom rules

## Local Testing

### Run tests locally:
```bash
# Run all tests
go test -v -race ./...

# With coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Test Docker build locally:
```bash
# Build for amd64 (matching production)
docker build --platform linux/amd64 --build-arg TARGETARCH=amd64 -t inventory-api:local .

# Run locally
docker run --rm -p 8020:8020 \
  -e GRPC_PORT=8020 \
  -e AWS_REGION=ap-northeast-2 \
  inventory-api:local
```

### Test with ECR:
```bash
# Login to ECR
aws ecr get-login-password --region ap-northeast-2 | \
  docker login --username AWS --password-stdin \
  137406935518.dkr.ecr.ap-northeast-2.amazonaws.com

# Tag and push
docker tag inventory-api:local \
  137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/traffic-tacos-inventory-api:test
docker push \
  137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/traffic-tacos-inventory-api:test
```

## Monitoring Deployment

After pushing to main, monitor the deployment progress:

### 1. GitHub Actions
```
https://github.com/traffic-tacos/inventory-api/actions
```
- Check workflow status
- View build logs
- Check Trivy scan results

### 2. Deployment Repo
```
https://github.com/traffic-tacos/deployment-repo/actions
```
- Verify manifest update
- Check auto-commit from bot

### 3. ArgoCD
```bash
# CLI
argocd app get inventory-api

# Or Web UI
https://argocd.traffictacos.store/applications/inventory-api
```

### 4. Kubernetes
```bash
# Check pods
kubectl get pods -n tacos-app -l app=inventory-api

# Watch rollout
kubectl rollout status deployment inventory-api -n tacos-app

# Check logs
kubectl logs -n tacos-app -l app=inventory-api --tail=100 -f
```

## CI/CD Pipeline Flow

```
┌─────────────────────────────────────────────────────────┐
│ Developer                                               │
├─────────────────────────────────────────────────────────┤
│ git commit -m "feat: add new feature"                  │
│ git push origin main                                    │
└─────────────────────────────────────────────────────────┘
                      ⬇
┌─────────────────────────────────────────────────────────┐
│ GitHub Actions: build.yml                               │
├─────────────────────────────────────────────────────────┤
│ 1. Test & Lint (~ 2 min)                               │
│    ├─ go mod verify                                     │
│    ├─ go test -race                                     │
│    └─ golangci-lint                                     │
│                                                         │
│ 2. Build & Push (~ 3 min)                              │
│    ├─ Docker build (amd64)                              │
│    ├─ Push to ECR                                       │
│    └─ Trivy scan                                        │
│                                                         │
│ 3. Deploy (~ 10 sec)                                    │
│    └─ Trigger deployment-repo                           │
└─────────────────────────────────────────────────────────┘
                      ⬇
┌─────────────────────────────────────────────────────────┐
│ deployment-repo: update-image-tag.yaml                  │
├─────────────────────────────────────────────────────────┤
│ 1. Update manifest (~ 5 sec)                           │
│    └─ sed image tag in deployment.yaml                 │
│                                                         │
│ 2. Commit & Push (~ 5 sec)                             │
│    └─ git commit + push                                 │
└─────────────────────────────────────────────────────────┘
                      ⬇
┌─────────────────────────────────────────────────────────┐
│ ArgoCD                                                  │
├─────────────────────────────────────────────────────────┤
│ 1. Detect Git change (~ 3 min)                         │
│ 2. Sync to cluster (~ 1 min)                           │
│ 3. Rolling update (~ 2 min)                            │
└─────────────────────────────────────────────────────────┘
                      ⬇
┌─────────────────────────────────────────────────────────┐
│ EKS Cluster                                             │
├─────────────────────────────────────────────────────────┤
│ ✅ New pods running                                     │
│ ✅ Old pods terminated                                  │
│ ✅ Service updated                                      │
└─────────────────────────────────────────────────────────┘

Total Time: ~ 12-15 minutes (commit to production)
```

## Best Practices

1. **Always create PRs:** Tests run automatically before merge
2. **Use semantic commits:** `feat:`, `fix:`, `chore:`, etc.
3. **Monitor ArgoCD:** Check deployment status after push
4. **Review security scans:** Check Trivy results in Actions
5. **Keep dependencies updated:** Regular `go get -u ./...`

## Security

- ✅ No long-term AWS credentials stored (OIDC)
- ✅ Minimal IAM permissions (ECR only)
- ✅ Container image scanning (Trivy)
- ✅ Dependency verification (go mod verify)
- ✅ Code scanning (golangci-lint)
- ✅ Distroless base image (minimal attack surface)
