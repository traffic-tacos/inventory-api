# GitHub Actions CI/CD Setup

## Overview

This repository uses GitHub Actions for automated building, testing, and deployment.

## Workflow Architecture

```
inventory-api (this repo)          deployment-repo
       │                                  │
       ├─ Code Push to main              │
       │                                  │
       ├─ Build Docker Image              │
       │  (linux/amd64)                   │
       │                                  │
       ├─ Push to ECR                     │
       │  tag: {git-sha}                  │
       │  tag: latest                     │
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

## Required Secrets

Configure the following secrets in this repository (Settings → Secrets and variables → Actions):

### 1. AWS_ROLE_ARN
AWS IAM Role ARN for OIDC authentication with GitHub Actions.

**Value example:**
```
arn:aws:iam::137406935518:role/GitHubActionsRole
```

**How to create:**
```bash
# Create IAM Role with OIDC provider for GitHub Actions
aws iam create-role \
  --role-name GitHubActionsRole \
  --assume-role-policy-document file://trust-policy.json

# Attach ECR permissions
aws iam attach-role-policy \
  --role-name GitHubActionsRole \
  --policy-arn arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPowerUser
```

**trust-policy.json:**
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
          "token.actions.githubusercontent.com:sub": "repo:traffic-tacos/inventory-api:*"
        }
      }
    }
  ]
}
```

### 2. DEPLOYMENT_REPO_TOKEN
GitHub Personal Access Token (PAT) with `repo` scope to trigger deployment-repo workflows.

**How to create:**
1. Go to GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Click "Generate new token (classic)"
3. Give it a descriptive name: `inventory-api-deployment-trigger`
4. Select scopes:
   - ✅ `repo` (Full control of private repositories)
5. Generate and copy the token
6. Add it to this repository's secrets as `DEPLOYMENT_REPO_TOKEN`

**Note:** Use fine-grained tokens for better security:
- Repository access: Only select `traffic-tacos/deployment-repo`
- Permissions:
  - Contents: Read and write
  - Metadata: Read-only

## Workflow Files

### `build-and-deploy.yaml`
Main CI/CD workflow that:
1. Builds Docker image (amd64 architecture)
2. Pushes to Amazon ECR
3. Scans image with Trivy
4. Triggers deployment-repo update (on main branch only)

**Triggers:**
- Push to `main` or `develop` branches
- Pull requests to `main` or `develop` branches

## Environment Variables

The workflow uses these environment variables:

| Variable | Value | Description |
|----------|-------|-------------|
| `AWS_REGION` | `ap-northeast-2` | AWS region for ECR |
| `ECR_REGISTRY` | `137406935518.dkr.ecr.ap-northeast-2.amazonaws.com` | ECR registry URL |
| `SERVICE_NAME` | `inventory-api` | Service name for tagging |
| `DEPLOYMENT_REPO` | `traffic-tacos/deployment-repo` | Target deployment repo |

## Image Tagging Strategy

- **Main branch:** `{git-short-sha}` (e.g., `a1b2c3d`)
- **Other branches:** `{branch-name}-{git-short-sha}` (e.g., `develop-a1b2c3d`)
- **Latest:** Always tagged on successful builds from any branch

## Manual Workflow Dispatch

You can manually trigger the workflow:

```bash
# Using GitHub CLI
gh workflow run build-and-deploy.yaml

# Or via GitHub UI: Actions → Build and Deploy → Run workflow
```

## Troubleshooting

### Build fails with "exec format error"
- Ensure `TARGETARCH=amd64` is set in the build-args
- Check Dockerfile has `GOARCH=${TARGETARCH:-amd64}`

### ECR login fails
- Verify `AWS_ROLE_ARN` secret is correctly set
- Check IAM role has ECR permissions
- Ensure OIDC provider is configured in AWS

### Deployment trigger fails
- Verify `DEPLOYMENT_REPO_TOKEN` is valid and has `repo` scope
- Check deployment-repo has `repository_dispatch` workflow
- Confirm `DEPLOYMENT_REPO` variable is correct

## Local Testing

Test Docker build locally:

```bash
# Build for amd64 (matching production)
docker build --platform linux/amd64 --build-arg TARGETARCH=amd64 -t inventory-api:local .

# Run locally
docker run --rm -p 8020:8020 \
  -e GRPC_PORT=8020 \
  -e AWS_REGION=ap-northeast-2 \
  inventory-api:local
```

## Monitoring

- **GitHub Actions:** Monitor workflow runs in the Actions tab
- **ECR:** Check image pushed to ECR console
- **ArgoCD:** Verify deployment in ArgoCD UI
- **Kubernetes:** Check pods with `kubectl get pods -n tacos-app -l app=inventory-api`

