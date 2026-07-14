locals {
  # A configured zone_id is all we need: the apex domain (TLD) is resolved from
  # Cloudflare, so this works for any zone without a hand-maintained zone name.
  cloudflare_enabled    = var.cloudflare_zone_id != ""
  custom_domain_enabled = local.cloudflare_enabled
  zone_name             = local.cloudflare_enabled ? module.cloudflare_zone[0].name : ""
  webhook_domain        = "${var.cloudflare_record_name}.${local.zone_name}"
  api_domain            = "${var.api_record_name}.${local.zone_name}"

  # Cloudflare Pages needs an account id + a Git connection, so it is opt-in on
  # top of the DNS setup rather than implied by zone_id alone.
  pages_enabled = local.cloudflare_enabled && var.cloudflare_account_id != ""

  # ACM issues one validation record per SAN; each single-domain cert has one.
  webhook_cert_validation_option = local.custom_domain_enabled ? tolist(aws_acm_certificate.webhook[0].domain_validation_options)[0] : null
  api_cert_validation_option     = local.custom_domain_enabled ? tolist(aws_acm_certificate.api[0].domain_validation_options)[0] : null

  cert_validation_records = local.custom_domain_enabled ? [
    {
      id      = "webhook-cert-validation"
      name    = trimsuffix(trimsuffix(local.webhook_cert_validation_option.resource_record_name, "."), ".${local.zone_name}")
      type    = local.webhook_cert_validation_option.resource_record_type
      content = trimsuffix(local.webhook_cert_validation_option.resource_record_value, ".")
      ttl     = 300
      proxied = false # must resolve as a plain CNAME for ACM to see it
      comment = "ACM DNS validation for ${local.webhook_domain}"
    },
    {
      id      = "api-cert-validation"
      name    = trimsuffix(trimsuffix(local.api_cert_validation_option.resource_record_name, "."), ".${local.zone_name}")
      type    = local.api_cert_validation_option.resource_record_type
      content = trimsuffix(local.api_cert_validation_option.resource_record_value, ".")
      ttl     = 300
      proxied = false
      comment = "ACM DNS validation for ${local.api_domain}"
    },
  ] : []

  api_gateway_records = local.cloudflare_enabled ? [
    {
      id      = "webhook"
      name    = var.cloudflare_record_name
      type    = "CNAME"
      content = aws_apigatewayv2_domain_name.webhook[0].domain_name_configuration[0].target_domain_name
      ttl     = 1
      proxied = true
      comment = "WhatsApp webhook endpoint"
    },
    {
      id      = "api"
      name    = var.api_record_name
      type    = "CNAME"
      content = aws_apigatewayv2_domain_name.api[0].domain_name_configuration[0].target_domain_name
      ttl     = 1
      proxied = true
      comment = "Dashboard API endpoint"
    },
  ] : []
}

# Resolve the zone's apex domain from Cloudflare so the ACM cert + API Gateway
# custom domain below can be built for any TLD from just cloudflare_zone_id.
module "cloudflare_zone" {
  count   = local.cloudflare_enabled ? 1 : 0
  source  = "../../../modules/cloudflare_zone"
  zone_id = var.cloudflare_zone_id
}

module "assistant" {
  source = "../../../modules/api_gateway_lambda"

  project_name               = var.project_name
  environment                = var.environment
  lambda_zip_path            = var.lambda_zip_path
  dashboard_api_zip_path     = var.dashboard_api_zip_path
  webhook_secret             = var.webhook_secret
  webhook_secret_value       = var.webhook_secret_value
  jwt_secret_value           = var.jwt_secret_value
  gemini_api_key_value       = var.gemini_api_key_value
  meta_graph_api_token_value = var.meta_graph_api_token_value
}

# ---------------------------------------------------------------------------
# Optional API Gateway custom domains, apex resolved from zone_id:
#   webhook.<apex> -> WhatsApp webhook, api.<apex> -> dashboard API.
# Both map the same HTTP API/stage; routing is by path within the API.
#
# ACM certs are free (DNS validated), and there's no extra charge for an
# apigatewayv2 custom domain beyond normal request costs. This lets the
# Cloudflare zone run SSL/TLS mode "strict" against certs that actually
# match the hostnames, instead of "full" trusting AWS's *.execute-api cert.
#
# Validation is a two-step chain to avoid a dependency cycle: the validation
# CNAMEs must exist in Cloudflare *before* aws_acm_certificate_validation
# resolves, and each CNAME's target isn't known until *after* the domain
# name + validated cert exist.
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
  validation_record_fqdns = [trimsuffix(local.webhook_cert_validation_option.resource_record_name, ".")]

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

resource "aws_acm_certificate" "api" {
  count             = local.custom_domain_enabled ? 1 : 0
  domain_name       = local.api_domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_acm_certificate_validation" "api" {
  count                   = local.custom_domain_enabled ? 1 : 0
  certificate_arn         = aws_acm_certificate.api[0].arn
  validation_record_fqdns = [trimsuffix(local.api_cert_validation_option.resource_record_name, ".")]

  depends_on = [module.cloudflare_cert_validation]
}

resource "aws_apigatewayv2_domain_name" "api" {
  count       = local.custom_domain_enabled ? 1 : 0
  domain_name = local.api_domain

  domain_name_configuration {
    certificate_arn = aws_acm_certificate_validation.api[0].certificate_arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }
}

resource "aws_apigatewayv2_api_mapping" "api" {
  count       = local.custom_domain_enabled ? 1 : 0
  api_id      = module.assistant.api_id
  domain_name = aws_apigatewayv2_domain_name.api[0].id
  stage       = module.assistant.stage_name
}

module "cloudflare_dns" {
  count  = local.cloudflare_enabled ? 1 : 0
  source = "../../../modules/cloudflare_dns"

  zone_id = var.cloudflare_zone_id
  records = local.api_gateway_records
}

# ---------------------------------------------------------------------------
# Frontend (apps/web) on Cloudflare Pages at dashboard.<apex>, built on push.
# Opt-in: requires cloudflare_account_id and a GitHub account already linked to
# Cloudflare. VITE_API_URL points at the dashboard-api custom domain above.
# ---------------------------------------------------------------------------
module "cloudflare_pages" {
  count  = local.pages_enabled ? 1 : 0
  source = "../../../modules/cloudflare_pages"

  account_id        = var.cloudflare_account_id
  project_name      = "${var.project_name}-${var.environment}-dashboard"
  production_branch = var.pages_production_branch
  github_owner      = var.github_owner
  github_repo       = var.github_repo
  api_url           = "https://${local.api_domain}"
  zone_id           = var.cloudflare_zone_id
  custom_domain     = "${var.dashboard_record_name}.${local.zone_name}"
  record_name       = var.dashboard_record_name
}

# "strict": the webhook CNAME always points at the API Gateway custom domain,
# whose ACM cert matches the hostname.
resource "cloudflare_zone_setting" "ssl" {
  count      = local.cloudflare_enabled ? 1 : 0
  zone_id    = var.cloudflare_zone_id
  setting_id = "ssl"
  value      = "strict"
}
