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
