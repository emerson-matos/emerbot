output "id" {
  description = "Zone ID (echoed back for convenience)."
  value       = data.cloudflare_zone.this.zone_id
}

output "name" {
  description = "Apex domain name for the zone (ex: emerson.abc.br)."
  value       = data.cloudflare_zone.this.name
}
