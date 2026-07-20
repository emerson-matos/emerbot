output "api_url" {
  value = module.assistant.api_url
}

output "cloudflare_record_ids" {
  value = try(module.cloudflare_dns[0].record_ids, [])
}

output "dashboard_api_url" {
  description = "URL da dashboard-api via custom domain (quando cloudflare_zone_id está definido)."
  value       = local.cloudflare_enabled ? "https://${local.api_domain}" : module.assistant.api_url
}

output "dashboard_url" {
  description = "URL do frontend Cloudflare Pages (quando o Pages está habilitado)."
  value       = local.pages_enabled ? module.cloudflare_pages[0].custom_domain_url : null
}

output "cognito_user_pool_id" { value = module.cognito_dashboard.user_pool_id }
output "cognito_app_client_id" { value = module.cognito_dashboard.app_client_id }
