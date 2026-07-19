#!/bin/sh
# Sources the pool/client IDs cognito-init discovered/created (see
# docker/cognito-init/init.sh) and derives the JWKS/issuer URLs from them
# before handing off to the real binary. Needed because cognito-local
# generates its own pool/client IDs — they can't be hardcoded env vars.
set -e

. /shared/cognito.env

export COGNITO_JWKS_URL="http://cognito-local:9229/${COGNITO_USER_POOL_ID}/.well-known/jwks.json"
export COGNITO_ISSUER="http://localhost:9229/${COGNITO_USER_POOL_ID}"

exec /dashboard-api
