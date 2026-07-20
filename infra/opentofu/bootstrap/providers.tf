# One-time bootstrap: creates the S3 state bucket and the GitHub OIDC deploy
# role that CI assumes. Runs with LOCAL state (chicken-and-egg: it is what
# creates the remote backend). Requires admin AWS creds and OpenTofu >= 1.10.
terraform {
  required_version = ">= 1.10.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}
