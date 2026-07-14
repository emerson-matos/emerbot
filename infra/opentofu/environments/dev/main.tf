locals {
  custom_domain_enabled = var.cloudflare_zone_id != "" && var.cloudflare_zone_name != ""
  webhook_domain        = "${var.cloudflare_record_name}.${var.cloudflare_zone_name}"

  # ACM issues one validation record per SAN; a single-domain cert has one.
  cert_validation_option = local.custom_domain_enabled ? tolist(aws_acm_certificate.webhook[0].domain_validation_options)[0] : null

  cert_validation_records = local.custom_domain_enabled ? [{
    id      = "webhook-cert-validation"
    name    = trimsuffix(trimsuffix(local.cert_validation_option.resource_record_name, "."), ".${var.cloudflare_zone_name}")
    type    = local.cert_validation_option.resource_record_type
    content = trimsuffix(local.cert_validation_option.resource_record_value, ".")
    ttl     = 300
    proxied = false # must resolve as a plain CNAME for ACM to see it
    comment = "ACM DNS validation for ${local.webhook_domain}"
  }] : []

  webhook_records = var.cloudflare_zone_id != "" ? [{
    id      = "webhook"
    name    = var.cloudflare_record_name
    type    = "CNAME"
    content = local.custom_domain_enabled ? aws_apigatewayv2_domain_name.webhook[0].domain_name_configuration[0].target_domain_name : trimsuffix(replace(module.assistant.api_url, "https://", ""), "/")
    ttl     = 1
    proxied = true
    comment = "WhatsApp webhook endpoint"
  }] : []
}

module "assistant" {
  source = "../../../modules/api_gateway_lambda"

  project_name               = var.project_name
  environment                = var.environment
  lambda_zip_path            = var.lambda_zip_path
  dashboard_api_zip_path     = var.dashboard_api_zip_path
  webhook_secret_value       = var.webhook_secret_value
  jwt_secret_value           = var.jwt_secret_value
  gemini_api_key_value       = var.gemini_api_key_value
  meta_graph_api_token_value = var.meta_graph_api_token_value
}

# ---------------------------------------------------------------------------
# Optional API Gateway custom domain (webhook.<cloudflare_zone_name>).
#
# ACM certs are free (DNS validated), and there's no extra charge for an
# apigatewayv2 custom domain beyond normal request costs. This lets the
# Cloudflare zone run SSL/TLS mode "strict" against a cert that actually
# matches the hostname, instead of "full" trusting AWS's *.execute-api cert.
#
# Validation is a two-step chain to avoid a dependency cycle: the validation
# CNAME must exist in Cloudflare *before* aws_acm_certificate_validation
# resolves, and the webhook CNAME's target isn't known until *after* the
# domain name + validated cert exist.
# ---------------------------------------------------------------------------
resource "aws_acm_certificate" "webhook" {
  count             = local.custom_domain_enabled ? 1 : 0
  domain_name       = local.webhook_domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

module "cloudflare_cert_validation" {
  count  = local.custom_domain_enabled ? 1 : 0
  source = "../../../modules/cloudflare_dns"

  zone_id = var.cloudflare_zone_id
  records = local.cert_validation_records
}

resource "aws_acm_certificate_validation" "webhook" {
  count                   = local.custom_domain_enabled ? 1 : 0
  certificate_arn         = aws_acm_certificate.webhook[0].arn
  validation_record_fqdns = [trimsuffix(local.cert_validation_option.resource_record_name, ".")]

  depends_on = [module.cloudflare_cert_validation]
}

resource "aws_apigatewayv2_domain_name" "webhook" {
  count       = local.custom_domain_enabled ? 1 : 0
  domain_name = local.webhook_domain

  domain_name_configuration {
    certificate_arn = aws_acm_certificate_validation.webhook[0].certificate_arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }
}

resource "aws_apigatewayv2_api_mapping" "webhook" {
  count       = local.custom_domain_enabled ? 1 : 0
  api_id      = module.assistant.api_id
  domain_name = aws_apigatewayv2_domain_name.webhook[0].id
  stage       = module.assistant.stage_name
}

module "cloudflare_dns" {
  count  = var.cloudflare_zone_id != "" ? 1 : 0
  source = "../../../modules/cloudflare_dns"

  zone_id = var.cloudflare_zone_id
  records = local.webhook_records
}

# "strict" once the custom domain's cert matches the hostname; "full" as a
# fallback when only cloudflare_zone_id is set (no cloudflare_zone_name yet),
# since the CNAME then still points at the raw execute-api hostname.
resource "cloudflare_zone_setting" "ssl" {
  count      = var.cloudflare_zone_id != "" ? 1 : 0
  zone_id    = var.cloudflare_zone_id
  setting_id = "ssl"
  value      = local.custom_domain_enabled ? "strict" : "full"
}
