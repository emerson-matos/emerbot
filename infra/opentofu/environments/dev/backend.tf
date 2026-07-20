# Remote state on S3. Bootstrap the bucket + the GitHub OIDC deploy role once
# with infra/opentofu/bootstrap (see docs/deploy.md), then `make tofu-migrate-state`
# to move the existing local state up.
#
# use_lockfile keeps the lock as a .tflock object next to the state (OpenTofu
# >= 1.10) — no DynamoDB lock table to run 24/7, so this stays within the cost
# cap. The bucket name must match var.state_bucket_name in the bootstrap config.
terraform {
  backend "s3" {
    bucket       = "emerbot-dev-tofu-state"
    key          = "dev/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}
