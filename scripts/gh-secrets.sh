#!/usr/bin/env bash
# Push the deploy secrets into GitHub Actions from your local environment, so
# CI (.github/workflows/deploy.yml) can read them. Run once, then again whenever
# a value rotates.
#
# Prereqs: the `gh` CLI, authenticated (`gh auth login`).
#
# Load your local values first, then run this:
#   set -a && . ./.env && set +a     # or however you keep them
#   ./scripts/gh-secrets.sh
#
# Maps <local env var> -> <GitHub Actions secret name> (the TF_VAR_* the
# workflow expects). Required ones abort if unset; optional ones (Cloudflare)
# are skipped so the matching feature just stays off.
set -euo pipefail

REPO="${GH_REPO:-emerson-matos/emerbot}"

command -v gh >/dev/null || { echo "gh CLI not found — install it and run 'gh auth login'"; exit 1; }

# secret_name  local_env_var  required
rows=(
  "TF_VAR_WEBHOOK_SECRET             WEBHOOK_SECRET            yes"
  "TF_VAR_WEBHOOK_SECRET_VALUE       WEBHOOK_VERIFY_TOKEN      yes"
  "TF_VAR_GEMINI_API_KEY_VALUE       GEMINI_API_KEY            yes"
  "TF_VAR_META_GRAPH_API_TOKEN_VALUE META_GRAPH_API_TOKEN      yes"
  "TF_VAR_WHATSAPP_PHONE_NUMBER_ID   WHATSAPP_PHONE_NUMBER_ID  yes"
  "TF_VAR_CLOUDFLARE_ZONE_ID         CLOUDFLARE_ZONE_ID        no"
  "TF_VAR_CLOUDFLARE_ACCOUNT_ID      CLOUDFLARE_ACCOUNT_ID     no"
  "CLOUDFLARE_API_TOKEN              CLOUDFLARE_API_TOKEN      no"
)

for row in "${rows[@]}"; do
  read -r secret var required <<<"$row"
  val="${!var:-}"
  if [ -z "$val" ]; then
    [ "$required" = yes ] && { echo "ERROR: required env \$$var is unset (-> $secret)"; exit 1; }
    echo "skip  $secret  (\$$var unset)"
    continue
  fi
  printf '%s' "$val" | gh secret set "$secret" --repo "$REPO"
  echo "set   $secret  <- \$$var"
done

# The deploy-role ARN comes from the bootstrap outputs, not the env.
if command -v tofu >/dev/null && arn=$(tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn 2>/dev/null); then
  printf '%s' "$arn" | gh secret set AWS_DEPLOY_ROLE_ARN --repo "$REPO"
  echo "set   AWS_DEPLOY_ROLE_ARN  <- bootstrap output"
else
  echo
  echo "NOTE: set AWS_DEPLOY_ROLE_ARN manually (after 'make tofu-bootstrap'):"
  echo "  gh secret set AWS_DEPLOY_ROLE_ARN --repo $REPO \\"
  echo "    --body \"\$(tofu -chdir=infra/opentofu/bootstrap output -raw deploy_role_arn)\""
fi
