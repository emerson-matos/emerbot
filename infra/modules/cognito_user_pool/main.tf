locals {
  prefix = "${var.project_name}-${var.environment}"
}

resource "aws_cognito_user_pool" "dashboard" {
  name                     = "${local.prefix}-dashboard"
  user_pool_tier           = "LITE"
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  admin_create_user_config {
    allow_admin_create_user_only = true
  }

  password_policy {
    minimum_length                   = 12
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  schema {
    attribute_data_type = "String"
    mutable             = false
    name                = "email"
    required            = true
  }

  schema {
    attribute_data_type = "String"
    mutable             = true
    name                = "phone_number"
    required            = true
  }
}

resource "aws_cognito_user_pool_client" "dashboard" {
  name                          = "${local.prefix}-dashboard-web"
  user_pool_id                  = aws_cognito_user_pool.dashboard.id
  generate_secret               = false
  prevent_user_existence_errors = "ENABLED"
  explicit_auth_flows           = ["ALLOW_USER_PASSWORD_AUTH", "ALLOW_REFRESH_TOKEN_AUTH"]
  access_token_validity         = 1
  id_token_validity             = 1
  refresh_token_validity        = 7

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}
