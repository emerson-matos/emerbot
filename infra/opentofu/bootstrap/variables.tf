variable "aws_region" {
  type        = string
  default     = "us-east-1"
  description = "Região AWS do bucket de state e do papel de deploy."
}

variable "state_bucket_name" {
  type        = string
  default     = "emerbot-dev-tofu-state"
  description = <<-EOT
    Nome do bucket S3 que guarda o terraform.tfstate remoto. Precisa ser único
    globalmente e igual ao 'bucket' em environments/dev/backend.tf. Se o nome já
    estiver em uso em outra conta, troque aqui e no backend.tf.
  EOT
}

variable "github_owner" {
  type        = string
  default     = "emerson-matos"
  description = "Owner do repositório GitHub autorizado a assumir o papel de deploy via OIDC."
}

variable "github_repo" {
  type        = string
  default     = "emerbot"
  description = "Repositório GitHub autorizado a assumir o papel de deploy via OIDC."
}

variable "deploy_role_name" {
  type        = string
  default     = "emerbot-dev-deploy"
  description = "Nome do papel IAM que o GitHub Actions assume para dar deploy."
}

variable "create_oidc_provider" {
  type        = bool
  default     = true
  description = <<-EOT
    Cria o OIDC provider do GitHub Actions. Só pode existir um por conta AWS; se
    a conta já tiver um (outro projeto), defina como false e o papel reutiliza o
    provider existente.
  EOT
}
