# Cloudflare DNS

Cloudflare DNS is optional and disabled by default. The base AWS/OpenTofu stack stays usable without it.

## How It Works

- `infra/modules/cloudflare_zone` resolves the zone's apex domain (TLD) from the
  `zone_id` via the `cloudflare_zone` data source, so the stack works for any
  zone without hard-coding the domain name.
- `infra/modules/cloudflare_dns` manages `cloudflare_dns_record` resources.
- `infra/opentofu/environments/dev` only instantiates these when `cloudflare_zone_id` is set.

## Required Inputs

- `cloudflare_zone_id = "<zone id>"` — the only knob needed to enable everything below.
- `CLOUDFLARE_API_TOKEN` in the shell environment for the Cloudflare provider
  (needs **Zone:Read** so the apex domain can be resolved, plus DNS edit permissions).

## What gets provisioned

Setting `cloudflare_zone_id` provisions **one** API Gateway custom domain
(`infra/modules/api_custom_domain`), with `<apex>` resolved automatically
from the zone:

- `api.<apex>` (`api_record_name`) — serves both the dashboard API and the
  WhatsApp webhook (`/webhook`). Routing is by path within the HTTP
  API/stage, not by hostname, so a single domain covers both.

The module provisions:

- `aws_acm_certificate`, DNS-validated via a (non-proxied) CNAME created in the
  same Cloudflare zone.
- `aws_apigatewayv2_domain_name` (REGIONAL) using that validated cert.
- `aws_apigatewayv2_api_mapping` mapping the stage to it.
- A proxied CNAME pointing at the custom domain's regional target (not the raw
  `execute-api` hostname), and `cloudflare_zone_setting.ssl` set to `strict`.

The frontend (Cloudflare Pages at `dashboard.<apex>`) is a separate, opt-in
piece — see [cloudflare-pages.md](./cloudflare-pages.md).

Both the ACM certificate and the API Gateway custom domain are free (normal
request costs still apply), so this fits the `< R$20/mês` budget.

## Notes

- DNS changes cost nothing on Cloudflare's side.
- The apex domain is derived from `zone_id`; there is no separate zone-name input
  to keep in sync (that was removed — it was a drift hazard).
- SSL mode is `strict` because the CNAME always targets a custom domain whose ACM
  cert matches the hostname. (`flexible` would 521 against the HTTPS-only origin;
  `full` would trust a non-matching cert.)
- The `cloudflare_dns` module is generic enough to support additional records
  later without changing the AWS stack.

Reference:

- [Cloudflare provider DNS record docs](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/dns_record)
- [Cloudflare provider zone data source](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/data-sources/zone)
