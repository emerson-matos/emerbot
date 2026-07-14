variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "project_name" {
  type    = string
  default = "emerbot"
}

variable "environment" {
  type    = string
  default = "dev"
}

variable "lambda_zip_path" {
  type        = string
  description = "Caminho do artefato zip do webhook Lambda."
}

variable "dashboard_api_zip_path" {
  type        = string
  description = "Caminho do artefato zip do dashboard-api Lambda."
}

variable "webhook_secret" {
  type        = string
  sensitive   = true
  description = "Valor do segredo usado para validar o payload."
  default     = "local-dev-webhook-secret"
}

variable "webhook_secret_value" {
  type        = string
  sensitive   = true
  description = "Valor do segredo usado para validar o webhook."
  default     = "local-dev-webhook-secret"
}

variable "jwt_secret_value" {
  type        = string
  sensitive   = true
  description = "Segredo para assinar JWTs do dashboard."
  default     = "local-dev-jwt-secret"
}

variable "gemini_api_key_value" {
  type        = string
  sensitive   = true
  description = "API key do Gemini para parsing de mensagens do WhatsApp."
  default     = ""
}

variable "meta_graph_api_token_value" {
  type        = string
  sensitive   = true
  description = "Token da API do WhatsApp Business (Graph API)."
  default     = ""
}

variable "cloudflare_zone_id" {
  type        = string
  default     = ""
  description = <<-EOT
    Zone ID do Cloudflare. Quando definido, provisiona o custom domain do
    API Gateway (webhook.<apex>) com certificado ACM validado via DNS. O
    domínio apex é resolvido automaticamente a partir do zone_id, então a
    infra funciona para qualquer TLD sem informar o nome do domínio. Deixe
    em branco para não provisionar DNS/custom domain.
  EOT
}

variable "cloudflare_record_name" {
  type        = string
  default     = "webhook"
  description = "Nome do registro DNS do webhook que apontará para o API Gateway."
}

variable "api_record_name" {
  type        = string
  default     = "api"
  description = "Nome do registro DNS da dashboard-api (api.<apex>)."
}

variable "dashboard_record_name" {
  type        = string
  default     = "dashboard"
  description = "Nome do registro DNS do frontend Cloudflare Pages (dashboard.<apex>)."
}

variable "cloudflare_account_id" {
  type        = string
  default     = ""
  description = <<-EOT
    Cloudflare account ID. Quando definido (junto com cloudflare_zone_id),
    provisiona o projeto Cloudflare Pages do frontend. Requer uma conta
    GitHub já conectada ao Cloudflare (OAuth feito no dashboard). Deixe em
    branco para não provisionar o frontend.
  EOT
}

variable "github_owner" {
  type        = string
  default     = "emerson-matos"
  description = "Owner do repositório GitHub conectado ao Cloudflare Pages."
}

variable "github_repo" {
  type        = string
  default     = "emerbot"
  description = "Nome do repositório GitHub conectado ao Cloudflare Pages."
}

variable "pages_production_branch" {
  type        = string
  default     = "main"
  description = "Branch de produção do Cloudflare Pages."
}
