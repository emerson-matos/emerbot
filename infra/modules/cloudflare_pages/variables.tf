variable "account_id" {
  type        = string
  description = "Cloudflare account ID onde o projeto Pages é criado."

  validation {
    condition     = trimspace(var.account_id) != ""
    error_message = "account_id must be a non-empty Cloudflare account identifier."
  }
}

variable "project_name" {
  type        = string
  description = "Nome do projeto Cloudflare Pages (também vira <name>.pages.dev)."
}

variable "production_branch" {
  type        = string
  default     = "main"
  description = "Branch que dispara deploys de produção."
}

variable "github_owner" {
  type        = string
  description = "Owner/org do repositório GitHub conectado ao Cloudflare."
}

variable "github_repo" {
  type        = string
  description = "Nome do repositório GitHub conectado ao Cloudflare."
}

variable "root_dir" {
  type        = string
  default     = "apps/web"
  description = "Diretório onde o build roda (monorepo: subpasta do frontend)."
}

variable "build_command" {
  type        = string
  default     = "npm run build"
  description = "Comando de build executado pelo Cloudflare Pages."
}

variable "destination_dir" {
  type        = string
  default     = "dist"
  description = "Diretório de saída do build (relativo a root_dir)."
}

variable "api_url" {
  type        = string
  description = "Valor de VITE_API_URL injetado no build de produção."
}

variable "cognito_endpoint" {
  type        = string
  description = "Valor de VITE_COGNITO_ENDPOINT injetado no build (endpoint regional do Cognito IdP, ex: https://cognito-idp.<region>.amazonaws.com)."
}

variable "cognito_client_id" {
  type        = string
  description = "Valor de VITE_COGNITO_CLIENT_ID injetado no build (app client ID do Cognito user pool)."
}

variable "zone_id" {
  type        = string
  description = "Zone ID do Cloudflare para criar o CNAME do domínio customizado."
}

variable "custom_domain" {
  type        = string
  description = "Hostname completo do frontend (ex: dashboard.emerson.abc.br)."
}

variable "record_name" {
  type        = string
  description = "Nome relativo do registro DNS (ex: dashboard)."
}
