# ---------------------------------------------------------------------------
# The CI deploy role's permissions, versioned alongside what they authorise.
#
# The role itself (and its trust policy) lives in infra/opentofu/bootstrap,
# because identity genuinely is a one-time thing. What it is ALLOWED TO DO
# lives here, in the module GitHub Actions applies, so that a new resource and
# the permission it needs travel in the same PR and land on the same button
# press.
#
# The old split — resource here, permission in bootstrap — meant every new
# service failed mid-apply with AccessDenied until someone with admin
# credentials remembered to re-apply a second root module by hand. It went
# unnoticed from 2026-07-21 (events:* left the config but never the live role)
# until the payment-imports bucket walked into it.
#
# This works because the role already grants iam:PutRolePolicy on
# role/emerbot-*, which matches emerbot-dev-deploy itself: CI writes its own
# policy. aws_iam_role_policy creates via PutRolePolicy, an upsert by name, so
# this adopts the existing inline policy on the first apply — no import needed.
#
# !! ProjectIAM and StateBucket carry CI's ability to run at all: the first is
# !! what lets this very resource be applied, the second is what reaches the
# !! remote state. Removing either from here locks CI out of the account, and
# !! only the floor policy in bootstrap (or admin credentials) undoes that.
# ---------------------------------------------------------------------------
data "aws_caller_identity" "current" {}

locals {
  deploy_role_name = "${var.project_name}-${var.environment}-deploy"
  state_bucket_arn = "arn:aws:s3:::${var.state_bucket_name}"

  # Built from the variables rather than from module.assistant: referencing the
  # bucket would make the grant depend on the resource that needs the grant.
  imports_bucket_arn = "arn:aws:s3:::${var.project_name}-${var.environment}-payment-imports"
}

# Service-scoped rather than per-resource: this is a single-purpose dev account,
# not a shared one. Broaden/tighten as the stack grows.
data "aws_iam_policy_document" "deploy_permissions" {
  statement {
    sid    = "AppServices"
    effect = "Allow"
    actions = [
      "lambda:*",
      "apigateway:*",
      "dynamodb:*",
      "scheduler:*",
      "logs:*",
      "acm:*",
      "cognito-idp:*",
      # A fila de entrada do WhatsApp e sua DLQ (ADR-028). Sem isto o apply do
      # CI para no primeiro recurso da fila — o recurso e a permissão que ele
      # exige viajam na mesma PR, por convenção do repositório.
      "sqs:*",
      # O orçamento com alerta que é o gatilho da guarda de custo do ADR-028.
      # A API de Budgets é global (us-east-1) e não aceita recurso por nome no
      # plan, então fica aqui junto com o resto.
      "budgets:*",
    ]
    resources = ["*"]
  }

  # Only the IAM roles/policies this project creates. The emerbot-* wildcard
  # covers the deploy role itself — that is what makes this config self-applying.
  statement {
    sid    = "ProjectIAM"
    effect = "Allow"
    actions = [
      "iam:CreateRole",
      "iam:DeleteRole",
      "iam:GetRole",
      "iam:UpdateRole",
      "iam:UpdateAssumeRolePolicy",
      "iam:TagRole",
      "iam:UntagRole",
      "iam:ListRoleTags",
      "iam:ListRolePolicies",
      "iam:ListAttachedRolePolicies",
      "iam:ListInstanceProfilesForRole",
      "iam:PutRolePolicy",
      "iam:DeleteRolePolicy",
      "iam:GetRolePolicy",
      "iam:AttachRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PassRole",
    ]
    resources = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${var.project_name}-*"]
  }

  # Read AWS-managed policies (e.g. AWSLambdaBasicExecutionRole) during refresh.
  statement {
    sid    = "IAMReadManaged"
    effect = "Allow"
    actions = [
      "iam:GetPolicy",
      "iam:GetPolicyVersion",
    ]
    resources = ["*"]
  }

  # Remote state bucket (state object + the .tflock lock object).
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
      local.state_bucket_arn,
      "${local.state_bucket_arn}/*",
    ]
  }

  # The import-envelope bucket. Bucket-level management actions only: deploying
  # never needs to read or delete the envelopes, and those are financial
  # records, so s3:GetObject/DeleteObject are deliberately absent — a
  # compromised CI role can reconfigure the bucket but cannot exfiltrate a
  # single import. The importer Lambda's own role holds the GetObject grant.
  #
  # The Get* actions are not decoration: once the bucket is in state, every plan
  # re-reads it (the provider fetches acl, versioning, encryption, lifecycle,
  # policy, notification…) just to render the diff.
  statement {
    sid    = "AppBuckets"
    effect = "Allow"
    actions = [
      "s3:CreateBucket",
      "s3:DeleteBucket",
      "s3:ListBucket",
      "s3:GetBucketLocation",
      "s3:GetBucketTagging",
      "s3:PutBucketTagging",
      "s3:GetBucketPublicAccessBlock",
      "s3:PutBucketPublicAccessBlock",
      "s3:GetBucketVersioning",
      "s3:PutBucketVersioning",
      "s3:GetLifecycleConfiguration",
      "s3:PutLifecycleConfiguration",
      "s3:GetBucketPolicy",
      "s3:PutBucketPolicy",
      "s3:DeleteBucketPolicy",
      "s3:GetBucketNotification",
      "s3:PutBucketNotification",
      "s3:GetBucketAcl",
      "s3:GetBucketCORS",
      "s3:GetBucketWebsite",
      "s3:GetBucketLogging",
      "s3:GetBucketObjectLockConfiguration",
      "s3:GetBucketRequestPayment",
      "s3:GetReplicationConfiguration",
      "s3:GetAccelerateConfiguration",
      "s3:GetEncryptionConfiguration",
      "s3:PutEncryptionConfiguration",
    ]
    resources = [local.imports_bucket_arn]
  }
}

resource "aws_iam_role_policy" "deploy_permissions" {
  name   = "${local.deploy_role_name}-permissions"
  role   = local.deploy_role_name
  policy = data.aws_iam_policy_document.deploy_permissions.json
}
