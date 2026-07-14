---
name: deploy
description: Build the Lambda zips and apply the OpenTofu dev stack. Use when the user asks to deploy, ship, or apply infra changes to AWS.
disable-model-invocation: true
---

Deploy the emerbot dev stack to AWS. This has real side effects (provisions/updates AWS + Cloudflare resources) — only run when the user explicitly asks.

Steps:

1. Confirm the working tree builds and tests pass first: `make build && make test`. Stop and report if either fails.
2. Rebuild the Lambda artifacts: `make build-lambdas`. This cross-compiles arm64, renames the binary to `bootstrap`, and rewrites the zips in `infra/opentofu/environments/dev/.lambdas/`. This step is mandatory — `source_code_hash` tracks the zip, so without a fresh zip Tofu won't detect code changes.
3. Show the plan: `make tofu-plan`. Summarize what will change and pause for the user to confirm before applying.
4. On confirmation: `make tofu-apply`.
5. Report the resulting `api_url` output and any Cloudflare record changes.

Required `TF_VAR_*` secrets (`GEMINI_API_KEY`, `META_GRAPH_API_TOKEN`, etc.) must already be in the shell/`.env`. If `tofu-plan` errors on a missing variable, tell the user which one rather than inventing a value.
