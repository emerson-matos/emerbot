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

## Custom domain (recommended)

Set `cloudflare_zone_name = "<apex domain, e.g. emerson.abc.br>"` in addition
to `cloudflare_zone_id` to provision a real API Gateway custom domain:

- `aws_acm_certificate` for `webhook.<cloudflare_zone_name>`, DNS-validated
  via a (non-proxied) CNAME created in the same Cloudflare zone.
- `aws_apigatewayv2_domain_name` (REGIONAL) using that validated cert.
- `aws_apigatewayv2_api_mapping` mapping the `$default` stage to it.
- The webhook CNAME then points at the custom domain's regional target
  instead of the raw `execute-api` hostname, and `cloudflare_zone_setting.ssl`
  is automatically set to `strict`.

Both the ACM certificate and the API Gateway custom domain are free (normal
request costs still apply), so this fits the `< R$20/mês` budget.

If `cloudflare_zone_name` is left blank (only `cloudflare_zone_id` is set),
the stack falls back to the old behavior: the CNAME points straight at the
raw `execute-api` invoke URL and SSL mode is `full` instead of `strict`,
since AWS's default cert doesn't cover the custom hostname.

## Notes

- DNS changes cost nothing on Cloudflare's side.
- The module is generic enough to support additional records later without changing the AWS stack.
- Without `cloudflare_zone_name`: the webhook CNAME is proxied and points at
  the raw API Gateway invoke URL, whose cert doesn't cover the custom
  hostname. SSL mode must stay `full` — `flexible` causes a 521 (Cloudflare
  tries plain HTTP to an HTTPS-only origin) and `strict` causes a 526 (cert
  hostname mismatch).

Reference:

- [Cloudflare provider DNS record docs](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/dns_record)

