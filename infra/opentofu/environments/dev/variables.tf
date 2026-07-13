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

variable "webhook_secret_value" {
  type        = string
  sensitive   = true
  description = "Valor do segredo usado para validar o webhook."
}

variable "jwt_secret_value" {
  type        = string
  sensitive   = true
  description = "Segredo para assinar JWTs do dashboard."
}

variable "gemini_api_key_value" {
  type        = string
  sensitive   = true
  description = "API key do Gemini para parsing de mensagens do WhatsApp."
}

variable "meta_graph_api_token_value" {
  type        = string
  sensitive   = true
  description = "Token da API do WhatsApp Business (Graph API)."
  default     = ""
}

variable "cloudflare_zone_id" {
  type        = string
  description = "Zone ID do Cloudflare para os registros DNS."
}

variable "cloudflare_record_name" {
  type        = string
  default     = "webhook"
  description = "Nome do registro DNS que apontará para o API Gateway."
}
