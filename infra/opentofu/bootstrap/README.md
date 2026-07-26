# Bootstrap — remote state + CI deploy role

Run with admin credentials before the first CI deploy. Creates:

- the S3 bucket that holds the remote `terraform.tfstate`
  (versioned + encrypted, all public access blocked);
- the GitHub Actions OIDC provider;
- the `emerbot-dev-deploy` IAM role that `.github/workflows/deploy.yml`
  assumes — so CI never needs long-lived AWS keys — plus the small `-floor`
  policy described below.

This config keeps **local** state (it is what creates the remote backend), so
its `terraform.tfstate` stays on your machine — that is expected.

## What is deliberately *not* here

The deploy role's **permissions**. Those live in
`environments/dev/deploy_role.tf`, inside the stack CI applies, so that a new
resource and the permission it needs ship in one PR and land on one button
press. Keeping them here meant every new service failed mid-apply with
`AccessDenied` until someone with admin creds remembered to re-apply this second
root module by hand — which went unnoticed from 2026-07-21 until the
payment-imports bucket hit it.

What stays is `aws_iam_role_policy.floor`: state access plus
`iam:PutRolePolicy` on this one role. It is the way back in if a merge ever
drops those grants from the stack-managed policy, since IAM unions inline
policies. Minimal, and expected never to change.

So this config is now genuinely one-time per account, and nothing that tracks
the stack should be added to it.

## If the plan wants to create everything

The state here is local *and* gitignored, so it lives only on the machine that
first applied this config. Anywhere else, `tofu plan` sees an empty state and
proposes creating the bucket, provider and role that are already running —
`9 to add, 0 to destroy`. Do not apply that: it collides on
`EntityAlreadyExists` and leaves a partial state. Run `make tofu-bootstrap-adopt`
instead, which imports the live resources into the local state (a read as far as
AWS is concerned), then plan again.

## Usage

```sh
# from the repo root
make tofu-bootstrap          # tofu init + apply in this dir with your AWS creds
make tofu-bootstrap-plan     # read-only: does the live account match this module?

# then copy the role ARN into the repo's GitHub secrets as AWS_DEPLOY_ROLE_ARN
tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn
```

If the account already has a GitHub OIDC provider (another project), run with
`-var create_oidc_provider=false` so the role reuses the existing one.

See [`docs/deploy.md`](../../../docs/deploy.md) for the full deploy runbook
(state migration, GitHub secrets, shipping via the CI button).
