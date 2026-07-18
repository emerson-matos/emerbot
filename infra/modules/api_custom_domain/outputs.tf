output "domain_name" {
  value = aws_apigatewayv2_domain_name.this.domain_name
}

output "target_domain_name" {
  value = aws_apigatewayv2_domain_name.this.domain_name_configuration[0].target_domain_name
}
