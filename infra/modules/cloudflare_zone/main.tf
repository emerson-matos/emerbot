# Resolves a Cloudflare zone's apex domain name from its zone_id, so the
# environment doesn't have to carry the domain as a hand-maintained variable
# that can drift out of sync with the zone_id.
data "cloudflare_zone" "this" {
  zone_id = var.zone_id
}
