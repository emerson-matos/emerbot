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

variable "message_ttl_days" {
  type    = number
  default = 7
}

