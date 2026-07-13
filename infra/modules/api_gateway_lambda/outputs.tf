output "api_url" {
  value = aws_apigatewayv2_api.http.api_endpoint
}

output "financial_entries_table_name" {
  value = aws_dynamodb_table.financial_entries.name
}

output "users_table_name" {
  value = aws_dynamodb_table.users.name
}

