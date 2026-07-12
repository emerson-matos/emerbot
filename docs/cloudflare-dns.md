# Cloudflare DNS

Cloudflare DNS is optional and disabled by default. The base AWS/OpenTofu stack stays usable without it.

## How It Works

- `infra/modules/cloudflare_dns` manages `cloudflare_dns_record` resources.
- `infra/opentofu/environments/dev` only instantiates that module when `cloudflare_enabled = true`.
- The dev environment currently wires a single CNAME record to the API Gateway URL created by the AWS module.

## Required Inputs

- `cloudflare_enabled = true`
- `cloudflare_zone_id = "<zone id>"`
- `CLOUDFLARE_API_TOKEN` in the shell environment for the Cloudflare provider

## Notes

- DNS changes cost nothing on Cloudflare's side.
- The module is generic enough to support additional records later without changing the AWS stack.

Reference:

- [Cloudflare provider DNS record docs](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/dns_record)

