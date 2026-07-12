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

resource "aws_dynamodb_table" "messages" {
  name         = "${local.prefix}-messages"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "UserId"
  range_key    = "Timestamp"

  attribute {
    name = "UserId"
    type = "S"
  }

  attribute {
    name = "Timestamp"
    type = "S"
  }

  ttl {
    attribute_name = "ExpiresAt"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "memories" {
  name         = "${local.prefix}-memories"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "UserId"
  range_key    = "MemoryKey"

  attribute {
    name = "UserId"
    type = "S"
  }

  attribute {
    name = "MemoryKey"
    type = "S"
  }
}

resource "aws_secretsmanager_secret" "webhook_secret" {
  name = "${local.prefix}/webhook/secret"
}

resource "aws_secretsmanager_secret_version" "webhook_secret" {
  secret_id     = aws_secretsmanager_secret.webhook_secret.id
  secret_string = var.webhook_secret_value
}

resource "aws_lambda_function" "webhook" {
  function_name = "${local.prefix}-webhook"
  role          = aws_iam_role.lambda_exec.arn
  filename      = var.lambda_zip_path
  handler       = var.lambda_handler
  runtime       = var.lambda_runtime
  architectures = ["arm64"]
  timeout       = 10
  memory_size   = 128

  environment {
    variables = {
      MESSAGES_TABLE = aws_dynamodb_table.messages.name
      MEMORIES_TABLE = aws_dynamodb_table.memories.name
      WEBHOOK_SECRET = var.webhook_secret_value
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

resource "aws_apigatewayv2_route" "webhook" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "POST /webhook"
  target    = "integrations/${aws_apigatewayv2_integration.webhook.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "allow_apigw" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.webhook.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}
