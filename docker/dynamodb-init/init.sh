#!/bin/sh
# docker/dynamodb-init/init.sh
# Creates all DynamoDB tables for local development.
# Runs as a one-shot init container before app containers start.

set -e

ENDPOINT="${DYNAMODB_ENDPOINT:-http://dynamodb-local:8000}"
REGION="${AWS_REGION:-us-east-1}"

AWS="aws dynamodb --endpoint-url $ENDPOINT --region $REGION --no-cli-pager"

create_table() {
  echo "Creating table: $1"
  $AWS create-table "$@" 2>&1 | grep -v "ResourceInUseException" || true
}

# financial-entries table (single-table for entries + categories)
create_table \
  --table-name emerbot-local-financial-entries \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
    AttributeName=GSI1PK,AttributeType=S \
    AttributeName=GSI1SK,AttributeType=S \
    AttributeName=GSI2PK,AttributeType=S \
    AttributeName=GSI2SK,AttributeType=S \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST \
  --global-secondary-indexes \
    '[
      {
        "IndexName": "GSI1-Category",
        "KeySchema": [
          {"AttributeName":"GSI1PK","KeyType":"HASH"},
          {"AttributeName":"GSI1SK","KeyType":"RANGE"}
        ],
        "Projection": {"ProjectionType":"ALL"}
      },
      {
        "IndexName": "GSI2-Status",
        "KeySchema": [
          {"AttributeName":"GSI2PK","KeyType":"HASH"},
          {"AttributeName":"GSI2SK","KeyType":"RANGE"}
        ],
        "Projection": {"ProjectionType":"ALL"}
      }
    ]'

# conversations table (short-term chat history, one item per turn)
create_table \
  --table-name emerbot-local-conversations \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST

echo "All tables ready."
