#!/bin/sh
# Sources the client ID cognito-init discovered/created (see
# docker/cognito-init/init.sh) so Vite picks it up as VITE_COGNITO_CLIENT_ID.
set -e

. /shared/cognito.env

export VITE_COGNITO_CLIENT_ID="${COGNITO_CLIENT_ID}"

# /app/node_modules is an anonymous volume (see docker-compose.yml) so the
# image's own node_modules survives the ./apps/web:/app bind mount instead of
# being shadowed by the host's (different OS/arch). That volume persists
# across container recreation even after rebuilding the image, so a plain
# `--build` + recreate does NOT pick up a package.json dependency change —
# reconcile it here every start instead of relying on rebuilds alone.
npm ci --no-audit --no-fund

exec npx vite --host 0.0.0.0
