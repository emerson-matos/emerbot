#!/bin/sh
# Sources the client ID cognito-init discovered/created (see
# docker/cognito-init/init.sh) so Vite picks it up as VITE_COGNITO_CLIENT_ID.
set -e

. /shared/cognito.env

export VITE_COGNITO_CLIENT_ID="${COGNITO_CLIENT_ID}"

exec npx vite --host 0.0.0.0
