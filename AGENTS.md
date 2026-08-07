# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Emerbot: a WhatsApp AI assistant + financial dashboard ("Farmácia Financeira"), built serverless-first on AWS as a study project with a hard cost cap (~R$20/month). Go monorepo + React frontend, deployed as Lambdas behind API Gateway HTTP, DynamoDB for storage, DNS/SSL via Cloudflare, provisioned with **OpenTofu**. Docs and Tofu variable descriptions are in **Portuguese**; ADRs live in `docs/adr/`.

## Toolchain

- Dev shell is provided by Nix: `nix develop` (or direnv `use flake`). CGO is disabled.
- Use **`tofu`**, not `terraform`. Use **`podman compose`**, not `docker compose`.
- `TMPDIR` is redirected to `$HOME/.tmp/buildah` (see `.envrc`) because `/` is full on the dev machine — keep this when running container/buildah commands.

## Commands (via root Makefile)

- `make build` / `make test` — `go build ./...` / `go test ./...`. Single test: `go test ./packages/finance -run TestName`.
- `make fmt` — formats Go with **gofumpt** (stricter than gofmt). CI does NOT run this; always run it before committing Go changes.
- `make lint` — `golangci-lint run ./...`. Web lint: `npm --prefix apps/web run lint` (**oxlint**, not ESLint).
- `make demo` — brings up podman compose stack + seeds ~120 demo entries. Demo login: `demo@user.com` / `fake123`. Native runs: `make run-webhook`, `make run-api`, `make run-cli`.

## Deploy / infra

- `make build-lambdas` cross-compiles `GOOS=linux GOARCH=arm64` (reproducibly: `-trimpath`, `-buildvcs=false`, CGO off, zeroed zip mtime), names the binary `bootstrap`, zips it into `infra/opentofu/environments/dev/.lambdas/`. Lambdas run on `provided.al2023`/arm64.
- `make tofu-plan` / `make tofu-apply` — depend on `build-lambdas` first and inject AWS creds via `aws configure export-credentials`. The zips list the Go sources as prerequisites, so they **rebuild automatically** whenever code changes — no need to `rm` them by hand anymore. Because the build is reproducible **on a given machine**, an unrelated rebuild that yields the same binary keeps `source_code_hash` stable, so Tofu only redeploys Lambdas whose code actually changed — a CI-to-CI property, which is what matters since deploys ship from there. A local break-glass apply rebuilds every zip differently (zip metadata, not the binary) and redeploys all four; see `docs/deploy.md`. `-buildvcs=false` is what makes that true: without it Go stamps the commit SHA (and a dirty-tree flag) into every binary, so all four Lambdas were redeployed on every apply regardless of what changed. (`make clean-lambdas` force-clears the zips if ever needed.)
- Prod secrets (`GEMINI_API_KEY`, `META_GRAPH_API_TOKEN`, etc.) are injected as **plain Lambda env vars** by OpenTofu from `TF_VAR_*` (see `infra/modules/api_gateway_lambda`) — there is no Secrets Manager.
- **Shipping**: deploys run from **GitHub Actions** (`.github/workflows/deploy.yml`) via **GitHub OIDC** (no stored AWS keys). PRs get a `tofu plan` comment; `apply` is a **manual button** (`workflow_dispatch`), never on merge. `make tofu-apply` still works locally as break-glass. Full runbook: `docs/deploy.md` (ADR-009).
- State is **remote in S3** (bucket `emerbot-dev-tofu-state`, native `use_lockfile` locking). One-time per account: `make tofu-bootstrap` (creates the bucket + OIDC deploy role, `infra/opentofu/bootstrap/`), then `make tofu-migrate-state`. Only a `dev` environment exists.
- **The deploy role's permissions are part of the stack**, in `infra/opentofu/environments/dev/deploy_role.tf` — not in `bootstrap/`. A resource and the IAM actions it needs belong in the same PR; CI rewrites its own inline policy on apply. Bootstrap holds only identity (role, trust, OIDC, state bucket) plus a minimal `-floor` policy that is the way back in if a merge ever drops `ProjectIAM`/`StateBucket`.

## Conventions

- **Conventional Commits** (`feat:`, `fix(infra):`, `refactor:`, `chore:`, `docs:`). Work on feature branches (`feat-*`, `fix/*`), merge via GitHub PRs to `main`.
- Go apps with HTTP handlers split entrypoints: `cmd/lambda` (Lambda handler) vs `cmd/local` (local HTTP server). Shared domain logic lives in `packages/`.

## Testing conventions (ADR-014)

- **DynamoDB stores take `dynamostore.API`, never `*dynamodb.Client`.** Each has a `…WithClient` constructor for injection; `dynamostore.NewClient` is the one place that builds a real client.
- **Test stores against `packages/dynamostore/dynamotest`**, an in-memory table that really evaluates key/filter/condition expressions, GSIs, sort order, `Limit`-before-filter and pagination. It **errors** on anything it does not model rather than silently matching — if you add a DynamoDB operation or expression, extend the fake. `dynamodb-local` (via `make demo`) stays the integration test.
- **Behaviour shared by several implementations of an interface is written once.** The finance summaries (including `MultiMonthlySummary`) live in `packages/finance/summaries.go` and both stores delegate; `store_conformance_test.go` runs the same scenario against every `Store` implementation, so a divergence is a red test rather than an environment-specific bug.
- **Consumers declare the narrow interface they use** (e.g. `notifier.LedgerReader`, `analytics.LedgerReader`, `finance.GoalStore`) instead of depending on the 18-method `finance.Store`.
- **Dates go through `packages/domain`**: `ParseMonth`, `ParseDay`, `CurrentMonth`, `CurrentMonthRange`. Malformed input is an error, never a silent fallback to the current period — on a financial view, "no data" and "you typed it wrong" must not render the same. Handlers read query params via `apps/dashboard-api/internal/httpx` (`DateRange`, `Month`).

## ADRs

Key decisions beyond the basics above:

- **ADR-015** (tool results never silently truncate): `list_due_entries` and `search_entries` always surface a `warning` when results are capped — never present partial totals as the full picture.
- **ADR-016** (revenue vs. cash-inflow are distinct): faturamento (`IsRevenue`, `Origin == venda`) and entradas de caixa (all income) are separate metrics. `Origin`, not category, decides.
- **ADR-017** ("today" is not a measurable day): the daily notification and analysis tools report through yesterday; today is not yet a fact.
- **ADR-018** (a week is the smallest comparable unit): month-over-month comparisons only activate after the first full week; before that, trends show the current month's own numbers without drawing conclusions.
- **ADR-019** (a flat daily average does not describe a month): no consumer — Go, dashboard, or the model — may divide what is missing by the days left. The only per-day ask is a `DayTarget`, scaled by the weekday's own Gaussian average; `DayTarget.State` names why there is no ask instead of going silent, and the agent prompt forbids the division outright.
- **ADR-020** (tomorrow is a day with a target too): a weekday's historical average is what it *usually* takes, never what it is being asked for. A question about one day must not be answered with the month — hence `caixa.amanha` beside `caixa.projecao_fim_do_mes`.
- **ADR-021** (a day's ask has a regime, and the day is a parameter): `Projection.Plan` spreads the gap over every remaining day at one shared factor, and `Analysis.DayTargetOn` prices any date from an assembled analysis — the `get_meta_do_dia` tool, not a field per day. `DayTarget.Basis` says whether the figures are `realizado` (a closed day: what it sold, and **no target** — today's plan must never be charged backwards), `em_curso` or `projetado`. `CashPosition.Forecast` is the projected curve the dashboard draws; the browser must never rebuild it.
- **ADR-022** (a total of bills is not a diagnosis): scheduled expenses are a liquidity question, so in the bot payload they live under `caixa` with `CashPosition.Commitments` (`coberto`/`descoberto`/`sem_historico`) beside them — derived from the projected trough, never from the amount. The health verdict is the backend's; no consumer adds concerns to it.
- **ADR-023** (a quiet day is an answer too): the daily digest goes out every day, to every registered recipient — there is no opt-in and no per-kind toggle (`NotificationPrefs` is an address book: user + phone), and the only thing that stops a message is having no phone on the Cognito account. "Nada vence hoje" comes from `notifications.Bills` (the ledger), never from an empty alert list, and the model writes from the whole insights JSON (`Analysis.DigestPayload`) under one rule: quote any figure, compute none. The day's outflows follow as a **second, model-free message** (`notifications.DueToday` + `billListMessages`) that splits across further messages rather than truncating — the digest carries the total, the list carries the items. That list goes out **twice a day**: with the digest, and again shortly after 15h (`RunOpenBills`) rebuilt from what is *still* unpaid. The afternoon run is only ever about the day's *pending* commitments and has no silent branch: the list, or one line saying nothing is pending. Which run it is comes from the scheduler's `input` (`ParseRunKind`), never from the clock, and each has its own dedupe key.
