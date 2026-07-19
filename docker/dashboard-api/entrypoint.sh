#!/bin/sh
# Sources the pool/client IDs cognito-init discovered/created (see
# docker/cognito-init/init.sh) and derives the JWKS/issuer URLs from them
# before handing off to the real binary. Needed because cognito-local
# generates its own pool/client IDs — they can't be hardcoded env vars.
set -e

. /shared/cognito.env

export COGNITO_JWKS_URL="http://cognito-local:9229/${COGNITO_USER_POOL_ID}/.well-known/jwks.json"
export COGNITO_ISSUER="http://localhost:9229/${COGNITO_USER_POOL_ID}"
# COGNITO_USER_POOL_ID/COGNITO_CLIENT_ID are set (not exported) by sourcing
# cognito.env above — fine for the interpolation on the two lines above since
# that happens in this shell, but exec below replaces the process, so
# COGNITO_CLIENT_ID itself needs an explicit export to survive that handoff.
export COGNITO_CLIENT_ID

exec /dashboard-api
