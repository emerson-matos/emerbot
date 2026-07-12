output "api_url" {
  value = aws_apigatewayv2_api.http.api_endpoint
}

output "messages_table_name" {
  value = aws_dynamodb_table.messages.name
}

output "memories_table_name" {
  value = aws_dynamodb_table.memories.name
}

output "financial_entries_table_name" {
  value = aws_dynamodb_table.financial_entries.name
}

output "users_table_name" {
  value = aws_dynamodb_table.users.name
}

