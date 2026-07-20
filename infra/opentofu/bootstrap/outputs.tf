output "deploy_role_arn" {
  description = "ARN do papel assumido pelo GitHub Actions. Configure como secret AWS_DEPLOY_ROLE_ARN no repositório."
  value       = aws_iam_role.deploy.arn
}

output "state_bucket" {
  description = "Bucket S3 do state remoto (precisa bater com o 'bucket' em environments/dev/backend.tf)."
  value       = aws_s3_bucket.state.bucket
}
