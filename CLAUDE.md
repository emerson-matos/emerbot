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

- `make build-lambdas` cross-compiles `GOOS=linux GOARCH=arm64`, renames the binary to `bootstrap`, zips it into `infra/opentofu/environments/dev/.lambdas/`. Lambdas run on `provided.al2`/arm64.
- `make tofu-plan` / `make tofu-apply` — depend on `build-lambdas` first and inject AWS creds via `aws configure export-credentials`. Because `source_code_hash` tracks the zip, **you must rebuild the zips before apply** or Tofu won't detect code changes.
- Prod secrets (`GEMINI_API_KEY`, `META_GRAPH_API_TOKEN`, etc.) are injected as **plain Lambda env vars** by OpenTofu from `TF_VAR_*` — trust the code in `infra/modules/api_gateway_lambda`, not the README's "Secrets Manager" mention (stale).
- Only a `dev` environment exists, with **local** `terraform.tfstate` (no remote backend).

## Conventions

- **Conventional Commits** (`feat:`, `fix(infra):`, `refactor:`, `chore:`, `docs:`). Work on feature branches (`feat-*`, `fix/*`), merge via GitHub PRs to `main`.
- Go apps split entrypoints: `cmd/lambda` (Lambda handler) vs `cmd/local` (local HTTP server). Shared domain logic lives in `packages/`.
