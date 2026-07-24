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

variable "notifier_zip_path" {
  type        = string
  description = "Caminho do artefato zip do notifier Lambda (alertas por WhatsApp)."
}

variable "lambda_handler" {
  type    = string
  default = "bootstrap"
}

variable "lambda_runtime" {
  type    = string
  default = "provided.al2023"
}

variable "webhook_secret" {
  type      = string
  sensitive = true
}

variable "webhook_secret_value" {
  type      = string
  sensitive = true
}

variable "cognito_user_pool_issuer" {
  type        = string
  description = "Issuer do User Pool Cognito que autentica o dashboard."
}

variable "cognito_app_client_id" {
  type        = string
  description = "ID do app client Cognito público do dashboard."
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
}

variable "whatsapp_phone_number_id" {
  type        = string
  default     = ""
  description = "Phone number ID do WhatsApp Business, remetente dos alertas proativos do notifier."
}

variable "notifier_schedule" {
  type        = string
  default     = "cron(0 9 * * ? *)"
  description = "Expressão de agenda do EventBridge para o notifier. Padrão: 09h São Paulo."
}

variable "notifier_timezone" {
  type        = string
  default     = "America/Sao_Paulo"
  description = "Fuso horário que define o dia (\"vence hoje\") dos alertas do notifier."
}

variable "dashboard_origin" {
  type        = string
  default     = ""
  description = <<-EOT
    Origem (esquema+host, ex: https://dashboard.emerson.abc.br) do frontend
    autorizada via CORS na API Gateway. Precisa ser gerenciado pela própria
    API Gateway (não pelo Lambda) porque respostas 401/403 do JWT authorizer
    nunca chegam a invocar o Lambda — só a API Gateway pode anexar cabeçalhos
    CORS a essas respostas. Vazio desativa o CORS gerenciado pela API Gateway
    (dev sem domínio de frontend configurado).
  EOT
}
