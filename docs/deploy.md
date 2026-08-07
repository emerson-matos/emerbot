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

   > This creates the role but not its permissions: those live in
   > `environments/dev` and arrive with the first deploy — see
   > [granting CI a new permission](#granting-ci-a-new-permission).

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

The deploy role's permissions live in
`infra/opentofu/environments/dev/deploy_role.tf` — inside the stack CI applies,
not beside the role. So adding a new kind of resource is **one** apply: put the
resource and the actions it needs in the same PR, merge, press the button. The
plan comment shows the policy diff alongside the resource diff.

This works because the role may rewrite its own inline policy
(`ProjectIAM` grants `iam:PutRolePolicy` on `role/emerbot-*`, which matches
`emerbot-dev-deploy` itself), and `aws_iam_role_policy` writes via
`PutRolePolicy`, an upsert by name.

Two statements are load-bearing and must never leave that document:

- **`ProjectIAM`** — what lets CI apply the policy resource at all;
- **`StateBucket`** — without it OpenTofu cannot read or lock the state.

Drop either and CI locks itself out of the account. The recovery is
`aws_iam_role_policy.floor` in the bootstrap config: a second inline policy
granting state access plus `iam:PutRolePolicy` on this one role. IAM unions
inline policies, so the floor survives a broken permissions policy and CI can
apply its way back out. It only exists once bootstrap has been applied on the
account — until then, that mistake needs admin credentials to undo.

### Ordering on the very first apply

The policy resource and the resources it authorises have no dependency on each
other, so a run that introduces both may create the resource first and fail:

```
Error: creating S3 Bucket (emerbot-dev-payment-imports): api error AccessDenied:
User: …assumed-role/emerbot-dev-deploy/GitHubActions is not authorized to
perform: s3:CreateBucket …
```

Press **Run workflow** again; the grant landed in the first run, and apply
resumes from where it stopped. There is deliberately no `depends_on` forcing the
order — it would buy ordering but not IAM propagation (the grant still needs a
few seconds to take effect), so it would not actually deliver the guarantee it
looks like, while pushing `module.assistant`'s data sources to apply time and
making every PR plan noisier.

## What still needs admin credentials

Only what `infra/opentofu/bootstrap` owns, and none of it tracks the stack:

| Change | Where |
| --- | --- |
| The role's **permissions** | `environments/dev` — ships with the deploy ✅ |
| The role's **trust policy** (which repo/branch may assume it) | bootstrap, admin |
| The OIDC provider | bootstrap, admin |
| The state bucket itself | bootstrap, admin |
| The `-floor` policy | bootstrap, admin |

Bootstrap keeps **local** state, gitignored, so it lives only on the machine
that first applied it. Elsewhere `tofu plan` sees an empty state and proposes
creating resources that already exist (`9 to add, 0 to destroy`). Do not apply
that — it collides on `EntityAlreadyExists` and leaves a partial state. Run
`make tofu-bootstrap-adopt` first: it imports the live resources into the local
state (a read, as far as AWS is concerned; re-runnable) and then the plan tells
you the truth. `make tofu-bootstrap-plan` is the read-only check.

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

### Um apply local redeploya todos os Lambdas

Esperado, e vale saber antes de assustar com o plan. O build é determinístico
**na mesma máquina**: `make clean-lambdas && go clean -cache && make
build-lambdas` duas vezes seguidas produz zips byte a byte idênticos. Entre
máquinas diferentes, não: os zips gerados aqui para o commit `3e4a6b6`, com o
mesmo Go 1.25.0 que o runner usa, têm sha256 diferente dos quatro que o CI
aplicou para esse mesmo commit.

A diferença é quase certamente do contêiner zip (modo do arquivo, versão do
Info-ZIP) e não do binário — o `-trimpath` e o `CGO_ENABLED=0` cuidam do
binário, e a data já é zerada com `touch -d @0`. Consequência prática: um
`make tofu-apply` de emergência mostra os quatro Lambdas com
`source_code_hash` mudando mesmo sem nenhuma linha de Go ter mudado. É barulho,
não risco: o código é o mesmo, só publica uma versão nova de cada função.

A propriedade que o CLAUDE.md descreve — "só redeploya o que mudou de verdade"
— é portanto entre execuções do CI, que sempre usam a mesma imagem de runner.
É a que importa, porque é de lá que os deploys saem, e está verificada: o plan
do PR #92, cujo único arquivo Go é um `_test.go` (fora do `GO_SOURCES`, logo
fora dos binários), rodou `make build-lambdas` num runner limpo e respondeu
`No changes` — os zips reconstruídos do zero bateram com os implantados no dia
anterior por outro runner.
