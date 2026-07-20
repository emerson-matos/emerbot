locals {
  # A configured zone_id is all we need: the apex domain (TLD) is resolved from
  # Cloudflare, so this works for any zone without a hand-maintained zone name.
  cloudflare_enabled = var.cloudflare_zone_id != ""
  zone_name          = local.cloudflare_enabled ? module.cloudflare_zone[0].name : ""
  api_domain         = "${var.api_record_name}.${local.zone_name}"
  # Cloudflare Pages needs an account id + a Git connection, so it is opt-in on
  # top of the DNS setup rather than implied by zone_id alone.
  pages_enabled = local.cloudflare_enabled && var.cloudflare_account_id != ""

  api_gateway_records = local.cloudflare_enabled ? [
    {
      id      = "api"
      name    = var.api_record_name
      type    = "CNAME"
      content = module.api_domain[0].target_domain_name
      ttl     = 1
      proxied = true
      comment = "Dashboard API endpoint (also serves /webhook by path)"
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
  notifier_zip_path          = var.notifier_zip_path
  webhook_secret             = var.webhook_secret
  webhook_secret_value       = var.webhook_secret_value
  cognito_user_pool_issuer   = module.cognito_dashboard.issuer
  cognito_app_client_id      = module.cognito_dashboard.app_client_id
  gemini_api_key_value       = var.gemini_api_key_value
  meta_graph_api_token_value = var.meta_graph_api_token_value
  whatsapp_phone_number_id   = var.whatsapp_phone_number_id
}

module "cognito_dashboard" {
  source       = "../../../modules/cognito_user_pool"
  project_name = var.project_name
  environment  = var.environment
}

# ---------------------------------------------------------------------------
# Optional API Gateway custom domain, apex resolved from zone_id: api.<apex>
# serves both the dashboard API and the WhatsApp webhook (/webhook) — routing
# is by path within the HTTP API/stage, not by hostname.
#
# ACM certs are free (DNS validated), and there's no extra charge for an
# apigatewayv2 custom domain beyond normal request costs. This lets the
# Cloudflare zone run SSL/TLS mode "strict" against a cert that actually
# matches the hostname, instead of "full" trusting AWS's *.execute-api cert.
# ---------------------------------------------------------------------------
module "api_domain" {
  count  = local.cloudflare_enabled ? 1 : 0
  source = "../../../modules/api_custom_domain"

  domain_name = local.api_domain
  zone_id     = var.cloudflare_zone_id
  zone_name   = local.zone_name
  api_id      = module.assistant.api_id
  stage_name  = module.assistant.stage_name
}

# api.<apex> used to be assembled inline in this file (one aws_acm_certificate
# + aws_apigatewayv2_domain_name + aws_apigatewayv2_api_mapping per domain,
# each with its own count = ... ? 1 : 0). Now that webhook.<apex> is gone,
# what's left is folded into modules/api_custom_domain so the optionality
# lives once, at this module call, instead of on every resource.
moved {
  from = aws_acm_certificate.api[0]
  to   = module.api_domain[0].aws_acm_certificate.this
}

moved {
  from = aws_acm_certificate_validation.api[0]
  to   = module.api_domain[0].aws_acm_certificate_validation.this
}

moved {
  from = aws_apigatewayv2_domain_name.api[0]
  to   = module.api_domain[0].aws_apigatewayv2_domain_name.this
}

moved {
  from = aws_apigatewayv2_api_mapping.api[0]
  to   = module.api_domain[0].aws_apigatewayv2_api_mapping.this
}

moved {
  from = module.cloudflare_cert_validation[0].cloudflare_dns_record.this["api-cert-validation"]
  to   = module.api_domain[0].cloudflare_dns_record.cert_validation
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
  cognito_endpoint  = module.cognito_dashboard.idp_endpoint
  cognito_client_id = module.cognito_dashboard.app_client_id
  zone_id           = var.cloudflare_zone_id
  custom_domain     = "${var.dashboard_record_name}.${local.zone_name}"
  record_name       = var.dashboard_record_name
}

# "strict": the CNAME always targets the API Gateway custom domain, whose ACM
# cert matches the hostname.
resource "cloudflare_zone_setting" "ssl" {
  count      = local.cloudflare_enabled ? 1 : 0
  zone_id    = var.cloudflare_zone_id
  setting_id = "ssl"
  value      = "strict"
}
