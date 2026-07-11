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
  description = "Caminho do artefato zip da Lambda."
}

variable "webhook_secret_value" {
  type        = string
  sensitive   = true
  description = "Valor do segredo usado para validar o webhook."
}

variable "cloudflare_enabled" {
  type        = bool
  default     = false
  description = "Habilita a criação opcional de DNS no Cloudflare."
}

variable "cloudflare_zone_id" {
  type        = string
  default     = ""
  description = "Zone ID do Cloudflare para os registros DNS opcionais."
}

variable "cloudflare_record_name" {
  type        = string
  default     = "webhook"
  description = "Nome do registro DNS que apontará para o API Gateway."
}
