---
name: deploy
description: Ship the emerbot dev stack to AWS — via the GitHub Actions deploy button (primary) or a local Tofu apply (break-glass). Use when the user asks to deploy, ship, or apply infra changes to AWS.
disable-model-invocation: true
---

Deploy the emerbot dev stack to AWS. This has real side effects (provisions/updates AWS + Cloudflare resources) — only run when the user explicitly asks. Full runbook: `docs/deploy.md`.

State is remote (S3) and CI authenticates via GitHub OIDC. Prefer the CI button; fall back to a local apply only when asked or when CI is unavailable.

## Primary path — GitHub Actions (manual button)

1. Confirm the change is merged (or on a PR whose `tofu plan` comment looks right).
2. Trigger the **deploy** workflow (`workflow_dispatch`) on `main` — via the GitHub MCP tools (`actions_run_trigger`) or by telling the user to click **Actions → deploy → Run workflow**.
3. Watch the run; the `apply` job builds the zips and runs `tofu apply`. Report the `Deploy outputs` (api_url) from the job summary.

Opening a PR automatically posts a `tofu plan` comment — summarize it for the user before they merge/ship.

## Break-glass — local apply

Only when explicitly requested or CI can't run:

1. `make build && make test`. Stop and report if either fails.
2. `make tofu-plan` — this rebuilds the Lambda zips automatically (they depend on the Go sources) and plans against the **remote** state. Summarize what will change and pause for confirmation.
3. On confirmation: `make tofu-apply`.
4. Report the resulting `api_url` output and any Cloudflare record changes.

Required `TF_VAR_*` secrets (`GEMINI_API_KEY`, `META_GRAPH_API_TOKEN`, etc.) must already be in the shell/`.env`; a new machine also needs `make tofu-init` once to configure the S3 backend. If Tofu errors on a missing variable, tell the user which one rather than inventing a value.
