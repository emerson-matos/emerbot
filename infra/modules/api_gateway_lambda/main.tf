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

resource "aws_dynamodb_table" "financial_entries" {
  name           = "${local.prefix}-financial-entries"
  billing_mode   = "PROVISIONED"
  hash_key       = "PK"
  range_key      = "SK"
  read_capacity  = 10
  write_capacity = 10

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

  # read/write capacity below sums to 25/25 with the base table above — the
  # DynamoDB Always-Free allowance (25 RCU/25 WCU) is per account, not per
  # table/index.
  global_secondary_index {
    name            = "GSI1-Category"
    hash_key        = "GSI1PK"
    range_key       = "GSI1SK"
    projection_type = "ALL"
    read_capacity   = 8
    write_capacity  = 8
  }

  global_secondary_index {
    name            = "GSI2-Status"
    hash_key        = "GSI2PK"
    range_key       = "GSI2SK"
    projection_type = "ALL"
    read_capacity   = 7
    write_capacity  = 7
  }
}

resource "aws_cloudwatch_log_group" "webhook" {
  name              = "/aws/lambda/${local.prefix}-webhook"
  retention_in_days = 14
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
      WEBHOOK_SECRET          = var.webhook_secret
      WEBHOOK_VERIFY_TOKEN    = var.webhook_secret_value
      FINANCIAL_ENTRIES_TABLE = aws_dynamodb_table.financial_entries.name
      META_GRAPH_API_TOKEN    = var.meta_graph_api_token_value
      GEMINI_API_KEY          = var.gemini_api_key_value
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
    webhook_get      = aws_apigatewayv2_route.webhook_get.route_key
    webhook_post     = aws_apigatewayv2_route.webhook_post.route_key
    dashboard        = [for route in aws_apigatewayv2_route.dashboard_protected : route.route_key]
    dashboard_public = [for route in aws_apigatewayv2_route.dashboard_public : route.route_key]
    webhook_int      = aws_apigatewayv2_integration.webhook.id
    dashboard_int    = aws_apigatewayv2_integration.dashboard_api.id
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
      ]
    }]
  })
}

resource "aws_cloudwatch_log_group" "dashboard_api" {
  name              = "/aws/lambda/${local.prefix}-dashboard-api"
  retention_in_days = 14
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
    }
  }
}

resource "aws_apigatewayv2_integration" "dashboard_api" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.dashboard_api.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_authorizer" "dashboard_jwt" {
  api_id           = aws_apigatewayv2_api.http.id
  authorizer_type  = "JWT"
  identity_sources = ["$request.header.Authorization"]
  name             = "${local.prefix}-dashboard-cognito"
  jwt_configuration {
    audience = [var.cognito_app_client_id]
    issuer   = var.cognito_user_pool_issuer
  }
}

# NOTE: these route lists must stay in sync with the mux registered in
# apps/dashboard-api/internal/app/app.go (newApp) — there is no compile-time
# link between the two. Adding/removing a route in one place should prompt
# checking the other.
locals {
  dashboard_protected_routes = toset([
    "GET /entries", "POST /entries", "PUT /entries/{id}", "DELETE /entries/{id}",
    "GET /summary/monthly", "GET /summary/categories", "GET /summary/cashflow",
    "GET /categories", "POST /categories", "GET /goals", "PUT /goals",
    "GET /notifications/preferences", "PUT /notifications/preferences",
  ])
  dashboard_public_routes = toset([
    "GET /health", "OPTIONS /{proxy+}",
  ])
}

resource "aws_apigatewayv2_route" "dashboard_protected" {
  for_each           = local.dashboard_protected_routes
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = each.value
  target             = "integrations/${aws_apigatewayv2_integration.dashboard_api.id}"
  authorizer_id      = aws_apigatewayv2_authorizer.dashboard_jwt.id
  authorization_type = "JWT"
  # "aws.cognito.signin.user.admin" is Cognito's implicit default scope granted
  # to access tokens minted via direct auth flows (USER_PASSWORD_AUTH), since
  # the app client (infra/modules/cognito_user_pool) sets no allowed_oauth_flows/
  # allowed_oauth_scopes — there's no custom resource-server scope to reference.
  authorization_scopes = ["aws.cognito.signin.user.admin"]
}

resource "aws_apigatewayv2_route" "dashboard_public" {
  for_each  = local.dashboard_public_routes
  api_id    = aws_apigatewayv2_api.http.id
  route_key = each.value
  target    = "integrations/${aws_apigatewayv2_integration.dashboard_api.id}"
}

resource "aws_lambda_permission" "allow_apigw_dashboard_api" {
  statement_id  = "AllowExecutionFromAPIGatewayDashboard"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.dashboard_api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

# ---------------------------------------------------------------------------
# Notifier Lambda — scheduled (EventBridge) daily WhatsApp digest. No API
# Gateway route: it is only ever invoked by the schedule below, never by HTTP.
# ---------------------------------------------------------------------------
resource "aws_iam_role" "notifier_exec" {
  name = "${local.prefix}-notifier-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "notifier_basic" {
  role       = aws_iam_role.notifier_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "notifier_dynamodb" {
  name = "${local.prefix}-notifier-dynamodb"
  role = aws_iam_role.notifier_exec.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
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

resource "aws_cloudwatch_log_group" "notifier" {
  name              = "/aws/lambda/${local.prefix}-notifier"
  retention_in_days = 14
}

resource "aws_lambda_function" "notifier" {
  function_name    = "${local.prefix}-notifier"
  role             = aws_iam_role.notifier_exec.arn
  filename         = var.notifier_zip_path
  source_code_hash = filebase64sha256(var.notifier_zip_path)
  handler          = var.lambda_handler
  runtime          = var.lambda_runtime
  architectures    = ["arm64"]
  # Scans prefs, reads entries and sends WhatsApp messages for every enabled
  # user — give it more room than the request-path Lambdas.
  timeout     = 60
  memory_size = 128

  environment {
    variables = {
      FINANCIAL_ENTRIES_TABLE  = aws_dynamodb_table.financial_entries.name
      META_GRAPH_API_TOKEN     = var.meta_graph_api_token_value
      WHATSAPP_PHONE_NUMBER_ID = var.whatsapp_phone_number_id
    }
  }
}

# EventBridge schedule: invoke the notifier once a day.
resource "aws_cloudwatch_event_rule" "notifier_daily" {
  name                = "${local.prefix}-notifier-daily"
  description         = "Dispara o notifier (alertas por WhatsApp) uma vez ao dia."
  schedule_expression = var.notifier_schedule
}

resource "aws_cloudwatch_event_target" "notifier_daily" {
  rule = aws_cloudwatch_event_rule.notifier_daily.name
  arn  = aws_lambda_function.notifier.arn
}

resource "aws_lambda_permission" "allow_eventbridge_notifier" {
  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.notifier.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.notifier_daily.arn
}
