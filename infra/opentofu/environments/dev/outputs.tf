output "api_url" {
  value = module.assistant.api_url
}

output "cloudflare_record_ids" {
  value = try(module.cloudflare_dns[0].record_ids, [])
}
