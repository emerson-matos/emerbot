# Bootstrap — remote state + CI deploy role

Run with admin credentials before the first CI deploy. Creates:

- the S3 bucket that holds the remote `terraform.tfstate`
  (versioned + encrypted, all public access blocked);
- the GitHub Actions OIDC provider;
- the `emerbot-dev-deploy` IAM role that `.github/workflows/deploy.yml`
  assumes — so CI never needs long-lived AWS keys.

This config keeps **local** state (it is what creates the remote backend), so
its `terraform.tfstate` stays on your machine — that is expected.

## This is not a one-time apply

The first two resources really are one-time, but the third is not: the deploy
role's permission policy lives here, while the resources it must be allowed to
touch live in `environments/dev`. Nothing re-applies this config on merge —
`make tofu-apply` and the deploy workflow both run `environments/dev` only. So
**adding a resource type to the stack and granting CI permission for it are two
separate applies**, and committing the grant does nothing until someone with
admin creds runs `make tofu-bootstrap` again.

Skipping that second apply fails at apply time in CI, not at plan time, and the
error names the missing action:

```
Error: creating S3 Bucket (emerbot-dev-payment-imports): api error AccessDenied:
User: arn:aws:sts::…:assumed-role/emerbot-dev-deploy/GitHubActions is not
authorized to perform: s3:CreateBucket … because no identity-based policy
allows the s3:CreateBucket action
```

If the action is already in `main.tf`, the policy is simply not applied yet:
run `make tofu-bootstrap` and re-run the deploy. `make tofu-bootstrap-plan` is
the read-only version — an empty plan means the live role matches this repo.

## Usage

```sh
# from the repo root
make tofu-bootstrap          # tofu init + apply in this dir with your AWS creds
make tofu-bootstrap-plan     # read-only drift check (does CI still have what it needs?)

# then copy the role ARN into the repo's GitHub secrets as AWS_DEPLOY_ROLE_ARN
tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn
```

If the account already has a GitHub OIDC provider (another project), run with
`-var create_oidc_provider=false` so the role reuses the existing one.

See [`docs/deploy.md`](../../../docs/deploy.md) for the full deploy runbook
(state migration, GitHub secrets, shipping via the CI button).
