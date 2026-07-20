output "user_pool_id" { value = aws_cognito_user_pool.dashboard.id }
output "app_client_id" { value = aws_cognito_user_pool_client.dashboard.id }
output "issuer" { value = "https://cognito-idp.${data.aws_region.current.name}.amazonaws.com/${aws_cognito_user_pool.dashboard.id}" }
# Base regional Cognito IdP endpoint (no pool ID suffix) — what the frontend's
# InitiateAuth calls POST to directly; the pool is implied by ClientId, not
# by the URL path (unlike issuer, which API Gateway's JWT authorizer needs
# with the pool ID appended).
output "idp_endpoint" { value = "https://cognito-idp.${data.aws_region.current.name}.amazonaws.com" }
