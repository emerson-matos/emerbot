output "api_url" {
  value = aws_apigatewayv2_api.http.api_endpoint
}

output "api_id" {
  value = aws_apigatewayv2_api.http.id
}

output "stage_name" {
  value = aws_apigatewayv2_stage.default.name
}

output "financial_entries_table_name" {
  value = aws_dynamodb_table.financial_entries.name
}

