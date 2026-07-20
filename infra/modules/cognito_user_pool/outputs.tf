output "user_pool_id" { value = aws_cognito_user_pool.dashboard.id }
output "app_client_id" { value = aws_cognito_user_pool_client.dashboard.id }
output "issuer" { value = "https://cognito-idp.${data.aws_region.current.name}.amazonaws.com/${aws_cognito_user_pool.dashboard.id}" }
