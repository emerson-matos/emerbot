variable "project_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "lambda_zip_path" {
  type        = string
  description = "Caminho do artefato zip do webhook Lambda."
}

variable "dashboard_api_zip_path" {
  type        = string
  description = "Caminho do artefato zip do dashboard-api Lambda."
}

variable "lambda_handler" {
  type    = string
  default = "bootstrap"
}

variable "lambda_runtime" {
  type    = string
  default = "provided.al2023"
}

variable "webhook_secret_value" {
  type      = string
  sensitive = true
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

