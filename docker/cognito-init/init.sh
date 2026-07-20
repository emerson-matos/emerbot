#!/bin/sh
# docker/cognito-init/init.sh
# Idempotently creates the local Cognito user pool/client and demo user
# against cognito-local, then writes the (dynamically generated) pool/client
# IDs to a shared file so dashboard-api and web can pick them up at startup.
#
# cognito-local doesn't support pinning a UserPoolId/ClientId up front —
# they're always server-generated on create-user-pool/create-user-pool-client,
# same as real Cognito. This script looks up existing ones by name first, so
# repeated `podman compose up` runs (with the cognito-local data volume
# persisted) reuse the same pool instead of creating a new one every time.

set -e

ENDPOINT="${COGNITO_ENDPOINT:-http://cognito-local:9229}"
REGION="${AWS_REGION:-us-east-1}"
POOL_NAME="emerbot-local"
CLIENT_NAME="emerbot-local-client"
DEMO_EMAIL="demo@user.com"
DEMO_PASSWORD="${USER_DEMO_PASSWORD:-fake123}"
SHARED_FILE="/shared/cognito.env"

AWS="aws --endpoint-url $ENDPOINT --region $REGION --no-cli-pager cognito-idp"

echo "Waiting for cognito-local at $ENDPOINT..."
until $AWS list-user-pools --max-results 1 >/dev/null 2>&1; do
  sleep 1
done

POOL_ID=$($AWS list-user-pools --max-results 60 \
  --query "UserPools[?Name=='$POOL_NAME'].Id | [0]" --output text)
if [ "$POOL_ID" = "None" ] || [ -z "$POOL_ID" ]; then
  echo "Creating user pool: $POOL_NAME"
  POOL_ID=$($AWS create-user-pool --pool-name "$POOL_NAME" \
    --username-attributes email \
    --query 'UserPool.Id' --output text)
fi
echo "User pool ID: $POOL_ID"

CLIENT_ID=$($AWS list-user-pool-clients --user-pool-id "$POOL_ID" --max-results 60 \
  --query "UserPoolClients[?ClientName=='$CLIENT_NAME'].ClientId | [0]" --output text)
if [ "$CLIENT_ID" = "None" ] || [ -z "$CLIENT_ID" ]; then
  echo "Creating user pool client: $CLIENT_NAME"
  CLIENT_ID=$($AWS create-user-pool-client --user-pool-id "$POOL_ID" --client-name "$CLIENT_NAME" \
    --explicit-auth-flows ALLOW_USER_PASSWORD_AUTH ALLOW_REFRESH_TOKEN_AUTH \
    --query 'UserPoolClient.ClientId' --output text)
fi
echo "User pool client ID: $CLIENT_ID"

# demo user — password policy is not enforced by cognito-local, so the local
# default ("fake123") is fine even though it wouldn't pass the real pool's
# policy (infra/modules/cognito_user_pool).
$AWS admin-create-user --user-pool-id "$POOL_ID" --username "$DEMO_EMAIL" \
  --user-attributes Name=email,Value="$DEMO_EMAIL" Name=name,Value=Demo Name=email_verified,Value=true \
  --message-action SUPPRESS >/dev/null 2>&1 || true
$AWS admin-set-user-password --user-pool-id "$POOL_ID" --username "$DEMO_EMAIL" \
  --password "$DEMO_PASSWORD" --permanent

mkdir -p "$(dirname "$SHARED_FILE")"
cat > "$SHARED_FILE" <<EOF
COGNITO_USER_POOL_ID=$POOL_ID
COGNITO_CLIENT_ID=$CLIENT_ID
EOF

echo "cognito-local ready: pool=$POOL_ID client=$CLIENT_ID (written to $SHARED_FILE)"
