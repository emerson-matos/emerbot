module "assistant" {
  source = "../../../modules/api_gateway_lambda"

  project_name           = var.project_name
  environment            = var.environment
  lambda_zip_path        = var.lambda_zip_path
  dashboard_api_zip_path = var.dashboard_api_zip_path
  webhook_secret_value   = var.webhook_secret_value
  jwt_secret_value       = var.jwt_secret_value
  gemini_api_key_value   = var.gemini_api_key_value
}

locals {
  cloudflare_records = var.cloudflare_enabled ? [{
    id      = "webhook"
    name    = var.cloudflare_record_name
    type    = "CNAME"
    content = module.assistant.api_url
    ttl     = 1
    proxied = false
    comment = "WhatsApp webhook endpoint"
  }] : []
}

module "cloudflare_dns" {
  count  = var.cloudflare_enabled ? 1 : 0
  source = "../../../modules/cloudflare_dns"

  zone_id = var.cloudflare_zone_id
  records = local.cloudflare_records
}
