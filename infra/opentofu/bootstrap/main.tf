data "aws_caller_identity" "current" {}

locals {
  oidc_host = "token.actions.githubusercontent.com"
  oidc_arn  = var.create_oidc_provider ? aws_iam_openid_connect_provider.github[0].arn : "arn:aws:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/${local.oidc_host}"
}

# ---------------------------------------------------------------------------
# Remote state bucket (versioned + encrypted, all public access blocked)
# ---------------------------------------------------------------------------
# Losing this bucket loses the record of every deployed resource, so no apply is
# allowed to plan its destruction — not a rename, not a stale-state mistake. To
# genuinely retire it, delete this block first, deliberately.
resource "aws_s3_bucket" "state" {
  bucket = var.state_bucket_name

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Versioning lets us roll back a bad state write, but old versions would pile up
# forever. Cap the history so storage stays a rounding error: keep the 10 most
# recent prior versions, and expire any that are also older than 30 days. S3
# only deletes a noncurrent version when BOTH conditions hold, so the 10 newest
# are always retained for rollback. Versioning must be enabled first.
resource "aws_s3_bucket_lifecycle_configuration" "state" {
  bucket     = aws_s3_bucket.state.id
  depends_on = [aws_s3_bucket_versioning.state]

  rule {
    id     = "expire-old-state-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days           = 30
      newer_noncurrent_versions = 10
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket                  = aws_s3_bucket.state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# The state holds secret values (Lambda env vars) in plaintext, so refuse any
# access that isn't over TLS. A deny-only policy doesn't grant public access,
# so it coexists with block_public_policy above.
data "aws_iam_policy_document" "state_bucket" {
  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.state.arn, "${aws_s3_bucket.state.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "state" {
  bucket = aws_s3_bucket.state.id
  policy = data.aws_iam_policy_document.state_bucket.json
}

# ---------------------------------------------------------------------------
# GitHub Actions OIDC provider + the role CI assumes (no long-lived AWS keys)
# ---------------------------------------------------------------------------
resource "aws_iam_openid_connect_provider" "github" {
  count          = var.create_oidc_provider ? 1 : 0
  url            = "https://${local.oidc_host}"
  client_id_list = ["sts.amazonaws.com"]
  # AWS validates GitHub's OIDC chain against its own trust store, so the
  # thumbprint is no longer security-relevant, but the field is still required.
  thumbprint_list = [
    "6938fd4d98bab03faadb97b34396831e3780aea1",
    "1c58a3a8518e8759bf075b76b750d4f2df264fcd",
  ]
}

data "aws_iam_policy_document" "deploy_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    principals {
      type        = "Federated"
      identifiers = [local.oidc_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_host}:aud"
      values   = ["sts.amazonaws.com"]
    }

    # Any branch or PR of this one repo. Tighten to a specific ref/environment
    # (e.g. "repo:owner/repo:ref:refs/heads/main") to lock down further.
    condition {
      test     = "StringLike"
      variable = "${local.oidc_host}:sub"
      values   = ["repo:${var.github_owner}/${var.github_repo}:*"]
    }
  }
}

# Destroying the role would lock CI out of the account until someone with admin
# creds recreated it and updated the AWS_DEPLOY_ROLE_ARN secret. The permission
# policy attached below is deliberately NOT protected: rewriting it in place is
# the routine change this config exists to make.
resource "aws_iam_role" "deploy" {
  name               = var.deploy_role_name
  assume_role_policy = data.aws_iam_policy_document.deploy_trust.json

  lifecycle {
    prevent_destroy = true
  }
}

# The permission policy proper is NOT here — it lives in environments/dev, in
# the module CI itself applies, so that a resource and the permission it needs
# ship in one PR (see that config's deploy_role.tf for the whole rationale).
#
# What stays is a floor: the minimum that lets CI reach the state and rewrite
# its own permissions. It exists for exactly one failure mode, the one that
# self-management introduces — a merge that drops ProjectIAM or StateBucket
# from the stack-managed policy would otherwise lock CI out of the account for
# good. IAM unions inline policies, so this floor holds regardless of what the
# other one says, and CI can always apply its way back out.
#
# Deliberately minimal, and expected never to change: everything that tracks
# the stack belongs in the policy the stack manages, not in here.
data "aws_iam_policy_document" "deploy_floor" {
  statement {
    sid    = "StateBucket"
    effect = "Allow"
    actions = [
      "s3:ListBucket",
      "s3:GetBucketVersioning",
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = [
      aws_s3_bucket.state.arn,
      "${aws_s3_bucket.state.arn}/*",
    ]
  }

  # Scoped to this role alone — the floor is a way back in, not a second set of
  # deploy permissions.
  statement {
    sid    = "SelfHeal"
    effect = "Allow"
    actions = [
      "iam:GetRolePolicy",
      "iam:PutRolePolicy",
    ]
    resources = [aws_iam_role.deploy.arn]
  }
}

resource "aws_iam_role_policy" "floor" {
  name   = "${var.deploy_role_name}-floor"
  role   = aws_iam_role.deploy.id
  policy = data.aws_iam_policy_document.deploy_floor.json
}

# `-permissions` moved to environments/dev. On a machine that still holds this
# module's old state, simply deleting the resource would plan a DeleteRolePolicy
# against the policy the dev stack now manages — stripping CI's permissions on
# the next bootstrap apply. Forget it from state instead and leave the live
# policy alone. Inert where the state is already empty.
removed {
  from = aws_iam_role_policy.deploy

  lifecycle {
    destroy = false
  }
}
