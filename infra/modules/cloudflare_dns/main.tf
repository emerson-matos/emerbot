locals {
  records = {
    for record in var.records : trimspace(record.id) => record
  }
}

resource "cloudflare_dns_record" "this" {
  for_each = local.records

  zone_id = var.zone_id
  name    = each.value.name
  type    = each.value.type
  content = each.value.content
  ttl     = each.value.ttl
  proxied = each.value.proxied
  comment = try(each.value.comment, null)
}

