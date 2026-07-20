# Cloudflare Pages project for the React frontend (apps/web), built directly by
# Cloudflare on push to the production branch (Git integration).
#
# Prerequisite that Terraform CANNOT provision: the GitHub account must already
# be authorized in Cloudflare (Pages > Connect to Git, a one-time OAuth link).
# This resource references that connection; it does not create it.
resource "cloudflare_pages_project" "this" {
  account_id        = var.account_id
  name              = var.project_name
  production_branch = var.production_branch

  build_config = {
    build_command   = var.build_command
    destination_dir = var.destination_dir
    root_dir        = var.root_dir
  }

  source = {
    type = "github"
    config = {
      owner                          = var.github_owner
      repo_name                      = var.github_repo
      production_branch              = var.production_branch
      production_deployments_enabled = true
      preview_deployment_setting     = "none"
    }
  }

  # production and preview must agree on fail_open (Cloudflare API code 8000066),
  # so both are defined explicitly with the same values and the same build-time
  # VITE_* vars. Vite inlines import.meta.env.VITE_* at build time — omitting
  # any of these here means the built bundle silently falls back to the
  # client's localhost dev default instead of erroring, so all three must
  # stay in sync (confirmed the hard way: a missing VITE_COGNITO_ENDPOINT
  # here shipped a production bundle pointed at localhost:9229).
  deployment_configs = {
    production = {
      fail_open = true
      env_vars = {
        VITE_API_URL = {
          type  = "plain_text"
          value = var.api_url
        }
        VITE_COGNITO_ENDPOINT = {
          type  = "plain_text"
          value = var.cognito_endpoint
        }
        VITE_COGNITO_CLIENT_ID = {
          type  = "plain_text"
          value = var.cognito_client_id
        }
      }
    }
    preview = {
      fail_open = true
      env_vars = {
        VITE_API_URL = {
          type  = "plain_text"
          value = var.api_url
        }
        VITE_COGNITO_ENDPOINT = {
          type  = "plain_text"
          value = var.cognito_endpoint
        }
        VITE_COGNITO_CLIENT_ID = {
          type  = "plain_text"
          value = var.cognito_client_id
        }
      }
    }
  }
}

# Attach the custom hostname to the project. Cloudflare validates it against the
# CNAME below; a Pages domain does NOT create its own DNS record.
resource "cloudflare_pages_domain" "this" {
  account_id   = var.account_id
  project_name = cloudflare_pages_project.this.name
  name         = var.custom_domain
}

resource "cloudflare_dns_record" "pages" {
  zone_id = var.zone_id
  name    = var.record_name
  type    = "CNAME"
  content = cloudflare_pages_project.this.subdomain
  ttl     = 1
  proxied = true
  comment = "Cloudflare Pages frontend (${var.project_name})"

  lifecycle {
    ignore_changes = [content]
  }
}
