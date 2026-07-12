output "record_ids" {
  value = [for record in cloudflare_dns_record.this : record.id]
}

output "record_names" {
  value = [for record in var.records : record.name]
}

