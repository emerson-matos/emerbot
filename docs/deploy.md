# Deploy runbook

How emerbot ships to AWS. Deploys run from **GitHub Actions** (manual button),
authenticated with **GitHub OIDC** — no long-lived AWS keys anywhere. State
lives in **S3**, not on a laptop. Everything here stays within the R$20/month
cap (S3 state is pennies; OIDC and Actions are free at this scale).

Pipeline: `.github/workflows/deploy.yml`.

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

   > **The bucket and the OIDC provider are one-time; the role's permissions are
   > not.** The deploy role's policy lives in the bootstrap config, which no
   > pipeline ever re-applies — see
   > [granting CI a new permission](#granting-ci-a-new-permission) below.

2. **Migrate existing local state to S3** (only if you were applying locally
   before — a fresh account can skip this):

   ```sh
   make tofu-migrate-state   # tofu init -migrate-state, answer "yes"
   ```

3. **Set the GitHub repository secrets.** These live only on your dev machine
   today (shell / `.env`); CI reads them from GitHub Actions secrets. Easiest is
   to load your env and push them in one shot:

   ```sh
   gh auth login                 # once
   set -a && . ./.env && set +a  # load your local values
   make gh-secrets               # uploads them with the right names (incl. AWS_DEPLOY_ROLE_ARN)
   ```

   `scripts/gh-secrets.sh` encodes the local-var → secret-name mapping below.
   Or set them by hand (Settings → Secrets and variables → Actions). Optional
   ones can be left unset — the matching feature just stays off (see the `""`
   defaults in `variables.tf`).

   | GitHub secret | From local env var | Required |
   | --- | --- | --- |
   | `AWS_DEPLOY_ROLE_ARN` | bootstrap output `deploy_role_arn` | ✅ |
   | `TF_VAR_WEBHOOK_SECRET` | `WEBHOOK_SECRET` (Meta app secret) | ✅ |
   | `TF_VAR_WEBHOOK_SECRET_VALUE` | `WEBHOOK_VERIFY_TOKEN` | ✅ |
   | `TF_VAR_GEMINI_API_KEY_VALUE` | `GEMINI_API_KEY` | ✅ |
   | `TF_VAR_META_GRAPH_API_TOKEN_VALUE` | `META_GRAPH_API_TOKEN` | ✅ |
   | `TF_VAR_WHATSAPP_PHONE_NUMBER_ID` | `WHATSAPP_PHONE_NUMBER_ID` | ✅ |
   | `CLOUDFLARE_API_TOKEN` | `CLOUDFLARE_API_TOKEN` | if using Cloudflare |
   | `TF_VAR_CLOUDFLARE_ZONE_ID` | `CLOUDFLARE_ZONE_ID` | if using Cloudflare |
   | `TF_VAR_CLOUDFLARE_ACCOUNT_ID` | `CLOUDFLARE_ACCOUNT_ID` | if using Pages |

   > The remote state is private (Block Public Access on, encrypted, TLS-only) —
   > but note it stores these secret values in plaintext, so treat read access
   > to the state bucket as equivalent to read access to the secrets.

## Shipping a change

1. Open a PR. Review the **Tofu plan** comment the pipeline posts.
2. Merge.
3. Go to **Actions → deploy → Run workflow** and run it on `main`. That applies.

## Granting CI a new permission

The deploy role is allowed a fixed set of AWS actions
(`infra/opentofu/bootstrap/main.tf`, `deploy_permissions`). Adding a new kind of
resource to `environments/dev` is therefore **two** applies, in two different
root modules, and only the first one happens on merge:

1. commit the resource *and* the matching actions in the bootstrap policy;
2. run `make tofu-bootstrap` with admin creds so the live role gains them;
3. then ship — **Actions → deploy → Run workflow**.

Do (3) without (2) and the plan looks perfect but the apply dies partway
through, naming the action it lacks:

```
Error: creating S3 Bucket (emerbot-dev-payment-imports): api error AccessDenied:
User: …assumed-role/emerbot-dev-deploy/GitHubActions is not authorized to
perform: s3:CreateBucket … because no identity-based policy allows the
s3:CreateBucket action
```

The fix is always the same: check the action is in the bootstrap policy, run
`make tofu-bootstrap`, re-run the deploy workflow. Apply is idempotent, so
re-running it after a partial failure just continues from where it stopped.

`make tofu-bootstrap-plan` answers "is the live role still in sync?" without
changing anything — an empty plan means it is. Worth running whenever a deploy
fails on `AccessDenied`, and before shipping a PR that touched `bootstrap/`.

## Importing acquirer data

Uploading an envelope to the imports bucket is what runs the importer:

```sh
make import-pagbank DIR=~/extracts/2026-07-23
```

See [payments-import.md](payments-import.md) for the full flow, including the
local path (which uses the same script).

## Recovering a failed payment import

The `payment-importer` Lambda is invoked asynchronously by S3, so a failure is
retried twice and then the event is gone. There is no dead-letter queue by
design — the envelope is still in the bucket, so nothing is actually lost, and
the log group is the record of what failed:

```sh
aws logs filter-log-events \
  --log-group-name /aws/lambda/emerbot-dev-payment-importer \
  --filter-pattern '"payment envelope import failed"'
```

Each failure logs the `bucket` and `key` of the envelope. To recover, re-upload
that object to the same key under `imports/` — the `ObjectCreated` event fires
again and the import replays. This is always safe: an import replaces exactly
its own `(provider, source day)` set, so re-running one converges on the same
state rather than duplicating rows.

A malformed envelope will keep failing until the envelope or the parser is
fixed, so read the logged error before replaying.

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
