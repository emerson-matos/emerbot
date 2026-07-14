output "project_name" {
  value = cloudflare_pages_project.this.name
}

output "pages_subdomain" {
  description = "Domínio *.pages.dev do projeto."
  value       = cloudflare_pages_project.this.subdomain
}

output "custom_domain_url" {
  value = "https://${var.custom_domain}"
}
