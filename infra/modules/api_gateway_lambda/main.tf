locals {
  prefix = "${var.project_name}-${var.environment}"
}

resource "aws_iam_role" "lambda_exec" {
  name = "${local.prefix}-webhook-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "basic_execution" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_region" "current" {}
data "aws_caller_identity" "current" {}

resource "aws_iam_role_policy" "read_webhook_parameters" {
  name = "${local.prefix}-read-webhook-parameters"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action   = ["ssm:GetParameter"]
      Effect   = "Allow"
      Resource = "arn:aws:ssm:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:parameter/${local.prefix}/*"
    }]
  })
}

resource "aws_dynamodb_table" "financial_entries" {
  name         = "${local.prefix}-financial-entries"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }
  attribute {
    name = "SK"
    type = "S"
  }
  attribute {
    name = "GSI1PK"
    type = "S"
  }
  attribute {
    name = "GSI1SK"
    type = "S"
  }
  attribute {
    name = "GSI2PK"
    type = "S"
  }
  attribute {
    name = "GSI2SK"
    type = "S"
  }

  global_secondary_index {
    name            = "GSI1-Category"
    hash_key        = "GSI1PK"
    range_key       = "GSI1SK"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "GSI2-Status"
    hash_key        = "GSI2PK"
    range_key       = "GSI2SK"
    projection_type = "ALL"
  }
}

resource "aws_dynamodb_table" "users" {
  name         = "${local.prefix}-users"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }
  attribute {
    name = "SK"
    type = "S"
  }
}

resource "aws_dynamodb_table" "refresh_tokens" {
  name         = "${local.prefix}-refresh-tokens"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "Token"

  attribute {
    name = "Token"
    type = "S"
  }

  ttl {
    attribute_name = "TTL"
    enabled        = true
  }
}

resource "aws_ssm_parameter" "webhook_secret" {
  name  = "/${local.prefix}/webhook/secret"
  type  = "SecureString"
  value = var.webhook_secret_value
}

resource "aws_ssm_parameter" "jwt_secret" {
  name  = "/${local.prefix}/jwt/secret"
  type  = "SecureString"
  value = var.jwt_secret_value
}

resource "aws_ssm_parameter" "gemini_api_key" {
  name  = "/${local.prefix}/gemini/api-key"
  type  = "SecureString"
  value = var.gemini_api_key_value
}

resource "aws_ssm_parameter" "meta_graph_api_token" {
  name  = "/${local.prefix}/meta/graph-api-token"
  type  = "SecureString"
  value = var.meta_graph_api_token_value
}

resource "aws_lambda_function" "webhook" {
  function_name    = "${local.prefix}-webhook"
  role             = aws_iam_role.lambda_exec.arn
  filename         = var.lambda_zip_path
  source_code_hash = filebase64sha256(var.lambda_zip_path)
  handler          = var.lambda_handler
  runtime          = var.lambda_runtime
  architectures    = ["arm64"]
  timeout          = 10
  memory_size      = 128

  environment {
    variables = {
      WEBHOOK_SECRET_PARAMETER       = aws_ssm_parameter.webhook_secret.name
      WEBHOOK_VERIFY_TOKEN           = var.webhook_secret_value
      FINANCIAL_ENTRIES_TABLE        = aws_dynamodb_table.financial_entries.name
      META_GRAPH_API_TOKEN_PARAMETER = aws_ssm_parameter.meta_graph_api_token.name
      GEMINI_API_KEY_PARAMETER       = aws_ssm_parameter.gemini_api_key.name
    }
  }
}

resource "aws_apigatewayv2_api" "http" {
  name          = "${local.prefix}-http"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "webhook" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.webhook.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "webhook_get" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "GET /webhook"
  target    = "integrations/${aws_apigatewayv2_integration.webhook.id}"
}

resource "aws_apigatewayv2_route" "webhook_post" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "POST /webhook"
  target    = "integrations/${aws_apigatewayv2_integration.webhook.id}"
}

locals {
  route_config = jsonencode({
    webhook_get   = aws_apigatewayv2_route.webhook_get.route_key
    webhook_post  = aws_apigatewayv2_route.webhook_post.route_key
    dashboard     = aws_apigatewayv2_route.dashboard_api.route_key
    webhook_int   = aws_apigatewayv2_integration.webhook.id
    dashboard_int = aws_apigatewayv2_integration.dashboard_api.id
  })
}

resource "aws_apigatewayv2_deployment" "this" {
  api_id = aws_apigatewayv2_api.http.id
  triggers = {
    config = local.route_config
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_apigatewayv2_stage" "default" {
  api_id        = aws_apigatewayv2_api.http.id
  name          = "$default"
  deployment_id = aws_apigatewayv2_deployment.this.id
}

resource "aws_lambda_permission" "allow_apigw" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.webhook.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

# ---------------------------------------------------------------------------
# IAM policy: webhook Lambda needs DynamoDB access to new tables
# ---------------------------------------------------------------------------
resource "aws_iam_role_policy" "webhook_dynamodb" {
  name = "${local.prefix}-webhook-dynamodb"
  role = aws_iam_role.lambda_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:Scan",
      ]
      Resource = [
        aws_dynamodb_table.financial_entries.arn,
        "${aws_dynamodb_table.financial_entries.arn}/index/*",
      ]
    }]
  })
}

# ---------------------------------------------------------------------------
# Dashboard API Lambda
# ---------------------------------------------------------------------------
resource "aws_iam_role" "dashboard_api_exec" {
  name = "${local.prefix}-dashboard-api-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "dashboard_api_basic" {
  role       = aws_iam_role.dashboard_api_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "dashboard_api_dynamodb" {
  name = "${local.prefix}-dashboard-api-dynamodb"
  role = aws_iam_role.dashboard_api_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:Scan",
      ]
      Resource = [
        aws_dynamodb_table.financial_entries.arn,
        "${aws_dynamodb_table.financial_entries.arn}/index/*",
        aws_dynamodb_table.users.arn,
        aws_dynamodb_table.refresh_tokens.arn,
      ]
    }]
  })
}

resource "aws_iam_role_policy" "dashboard_api_parameters" {
  name = "${local.prefix}-dashboard-api-parameters"
  role = aws_iam_role.dashboard_api_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameter"]
      Resource = "arn:aws:ssm:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:parameter/${local.prefix}/*"
    }]
  })
}

resource "aws_lambda_function" "dashboard_api" {
  function_name    = "${local.prefix}-dashboard-api"
  role             = aws_iam_role.dashboard_api_exec.arn
  filename         = var.dashboard_api_zip_path
  source_code_hash = filebase64sha256(var.dashboard_api_zip_path)
  handler          = var.lambda_handler
  runtime          = var.lambda_runtime
  architectures    = ["arm64"]
  timeout          = 10
  memory_size      = 128

  environment {
    variables = {
      FINANCIAL_ENTRIES_TABLE = aws_dynamodb_table.financial_entries.name
      USERS_TABLE             = aws_dynamodb_table.users.name
      REFRESH_TOKENS_TABLE    = aws_dynamodb_table.refresh_tokens.name
      JWT_SECRET_PARAMETER    = aws_ssm_parameter.jwt_secret.name
    }
  }
}

resource "aws_apigatewayv2_integration" "dashboard_api" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.dashboard_api.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "dashboard_api" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "ANY /{proxy+}"
  target    = "integrations/${aws_apigatewayv2_integration.dashboard_api.id}"
}

resource "aws_lambda_permission" "allow_apigw_dashboard_api" {
  statement_id  = "AllowExecutionFromAPIGatewayDashboard"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.dashboard_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}
