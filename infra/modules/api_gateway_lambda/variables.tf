variable "project_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "lambda_zip_path" {
  type = string
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
