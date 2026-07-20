# Bootstrap — remote state + CI deploy role

Run **once per AWS account**, with admin credentials, before the first CI
deploy. Creates:

- the S3 bucket that holds the remote `terraform.tfstate`
  (versioned + encrypted, all public access blocked);
- the GitHub Actions OIDC provider;
- the `emerbot-dev-deploy` IAM role that `.github/workflows/deploy.yml`
  assumes — so CI never needs long-lived AWS keys.

This config keeps **local** state (it is what creates the remote backend), so
its `terraform.tfstate` stays on your machine — that is expected.

## Usage

```sh
# from the repo root
make tofu-bootstrap          # tofu init + apply in this dir with your AWS creds

# then copy the role ARN into the repo's GitHub secrets as AWS_DEPLOY_ROLE_ARN
tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn
```

If the account already has a GitHub OIDC provider (another project), run with
`-var create_oidc_provider=false` so the role reuses the existing one.

See [`docs/deploy.md`](../../../docs/deploy.md) for the full deploy runbook
(state migration, GitHub secrets, shipping via the CI button).
