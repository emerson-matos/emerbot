# Deploy runbook

How emerbot ships to AWS. Deploys run from **GitHub Actions** (manual button),
authenticated with **GitHub OIDC** — no long-lived AWS keys anywhere. State
lives in **S3**, not on a laptop. Everything here stays within the R$20/month
cap (S3 state is pennies; OIDC and Actions are free at this scale).

Pipeline: `.github/workflows/deploy.yml`.

> **One-time install of the workflow file.** The pipeline definition lives in
> this repo as [`docs/deploy-workflow.yml`](deploy-workflow.yml) because it was
> committed from an environment whose token can't write under
> `.github/workflows/`. Put it in place once:
>
> ```sh
> git mv docs/deploy-workflow.yml .github/workflows/deploy.yml
> git commit -m "ci: add deploy workflow" && git push
> ```
>
> (or paste it into the GitHub web UI → **Add file** at
> `.github/workflows/deploy.yml`). A normal local push or the web UI has the
> `workflow` scope this needs.

- **Pull request** touching `apps/`, `packages/`, `infra/`, `go.*` or the
  `Makefile` → runs `tofu plan` and posts it as a PR comment. Never applies.
- **Actions → deploy → Run workflow** (`workflow_dispatch`) → builds the Lambda
  zips and runs `tofu apply`. This is the ship button.

## One-time setup (per AWS account)

1. **Bootstrap the backend + deploy role** with admin AWS creds:

   ```sh
   make tofu-bootstrap
   ```

   Creates the S3 state bucket (`emerbot-dev-tofu-state`), the GitHub OIDC
   provider, and the `emerbot-dev-deploy` IAM role. If the account already has a
   GitHub OIDC provider, re-run with `-var create_oidc_provider=false`
   (`tofu -chdir=infra/opentofu/bootstrap apply -var create_oidc_provider=false`).

   > If the bucket name is already taken globally, change `state_bucket_name` in
   > `infra/opentofu/bootstrap/variables.tf` **and** `bucket` in
   > `infra/opentofu/environments/dev/backend.tf` to match.

2. **Migrate existing local state to S3** (only if you were applying locally
   before — a fresh account can skip this):

   ```sh
   make tofu-migrate-state   # tofu init -migrate-state, answer "yes"
   ```

3. **Set the GitHub repository secrets** (Settings → Secrets and variables →
   Actions). Optional ones can be left unset — the matching feature just stays
   off (see the `""` defaults in `variables.tf`).

   | Secret | Required | Notes |
   | --- | --- | --- |
   | `AWS_DEPLOY_ROLE_ARN` | ✅ | `tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn` |
   | `TF_VAR_WEBHOOK_SECRET` | ✅ | WhatsApp webhook verify/secret |
   | `TF_VAR_WEBHOOK_SECRET_VALUE` | ✅ | verify token |
   | `TF_VAR_GEMINI_API_KEY_VALUE` | ✅ | Gemini API key |
   | `TF_VAR_META_GRAPH_API_TOKEN_VALUE` | ✅ | WhatsApp Graph API token |
   | `TF_VAR_WHATSAPP_PHONE_NUMBER_ID` | ✅ | notifier sender |
   | `CLOUDFLARE_API_TOKEN` | if using Cloudflare | DNS / Pages provider |
   | `TF_VAR_CLOUDFLARE_ZONE_ID` | if using Cloudflare | custom domain |
   | `TF_VAR_CLOUDFLARE_ACCOUNT_ID` | if using Pages | frontend |

## Shipping a change

1. Open a PR. Review the **Tofu plan** comment the pipeline posts.
2. Merge.
3. Go to **Actions → deploy → Run workflow** and run it on `main`. That applies.

## Break-glass: deploy from your machine

The Makefile still drives Tofu locally against the same remote state, for when
CI is unavailable:

```sh
make tofu-init     # first time on a new machine (configures the S3 backend)
make tofu-plan
make tofu-apply
```

Uses your local AWS profile via `aws configure export-credentials`, and the
`TF_VAR_*` secrets from your shell/`.env`. `make build-lambdas` runs
automatically as a prerequisite.
