# Cloudflare Pages (frontend)

The React dashboard (`apps/web`) is served from Cloudflare Pages at
`dashboard.<apex>`, built directly by Cloudflare on every push to the production
branch (Git integration). Managed by `infra/modules/cloudflare_pages`.

Like the rest of the Cloudflare setup, it's optional and disabled by default.

## Prerequisite (one-time, manual)

Terraform can create the Pages **project** but cannot create the GitHub↔Cloudflare
account link. Do this once in the Cloudflare dashboard:

> Pages → Create → Connect to Git → authorize your GitHub account.

After that, OpenTofu manages the project, build config, env vars, custom domain,
and DNS.

## Required Inputs

- `cloudflare_zone_id` — enables the DNS/custom-domain layer (see
  [cloudflare-dns.md](./cloudflare-dns.md)).
- `cloudflare_account_id` — the Cloudflare account that owns the Pages project.
  This is what turns the frontend on (`pages_enabled = zone_id && account_id`).
- `CLOUDFLARE_API_TOKEN` in the environment, with Pages edit permission.
- Defaults you can override: `github_owner` (`emerson-matos`), `github_repo`
  (`emerbot`), `pages_production_branch` (`main`), `dashboard_record_name`
  (`dashboard`).

## What gets provisioned

- `cloudflare_pages_project` — Git-connected, building `apps/web`:
  - `root_dir = apps/web`, `build_command = npm run build`, output `dist`.
  - Production env var `VITE_API_URL = https://api.<apex>` (the dashboard-api
    custom domain), injected at build time.
- `cloudflare_pages_domain` — attaches `dashboard.<apex>` to the project.
- A proxied CNAME `dashboard.<apex> → <project>.pages.dev` (a Pages domain does
  not create its own DNS record).

## Notes

- The SPA calls `api.<apex>`; the dashboard-api must allow that browser origin
  (`https://dashboard.<apex>`) via CORS — that's application config, not infra.
- Project name is `<project_name>-<environment>-dashboard` (e.g.
  `emerbot-dev-dashboard`), which is also the `*.pages.dev` subdomain.

Reference:

- [cloudflare_pages_project](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/pages_project)
- [cloudflare_pages_domain](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/pages_domain)
