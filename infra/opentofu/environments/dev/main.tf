module "assistant" {
  source = "../../../modules/api_gateway_lambda"

  project_name    = var.project_name
  environment     = var.environment
  lambda_zip_path = var.lambda_zip_path
}

