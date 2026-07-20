---
name: deploy
description: Build the Lambda zips and apply the OpenTofu dev stack. Use when the user asks to deploy, ship, or apply infra changes to AWS.
disable-model-invocation: true
---

Deploy the emerbot dev stack to AWS. This has real side effects (provisions/updates AWS + Cloudflare resources) — only run when the user explicitly asks.

Steps:

1. Confirm the working tree builds and tests pass first: `make build && make test`. Stop and report if either fails.
2. Rebuild the Lambda artifacts: `make build-lambdas`. This cross-compiles arm64 to a `bootstrap` binary and (re)writes the zips in `infra/opentofu/environments/dev/.lambdas/`. The zips depend on the Go sources, so they rebuild automatically when code changes — no manual `rm` needed. `make tofu-plan`/`tofu-apply` run this for you, so this step is really just to surface build errors early. (Use `make clean-lambdas` only if you need to force a from-scratch rebuild.)
3. Show the plan: `make tofu-plan`. Summarize what will change and pause for the user to confirm before applying.
4. On confirmation: `make tofu-apply`.
5. Report the resulting `api_url` output and any Cloudflare record changes.

Required `TF_VAR_*` secrets (`GEMINI_API_KEY`, `META_GRAPH_API_TOKEN`, etc.) must already be in the shell/`.env`. If `tofu-plan` errors on a missing variable, tell the user which one rather than inventing a value.
