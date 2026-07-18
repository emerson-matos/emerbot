variable "domain_name" {
  type        = string
  description = "FQDN for the API Gateway custom domain (DNS-validated ACM cert)."
}

variable "zone_id" {
  type        = string
  description = "Cloudflare zone ID where the ACM validation CNAME is created."
}

variable "zone_name" {
  type        = string
  description = "Cloudflare zone apex domain, used to trim the ACM validation record name to a Cloudflare-relative name."
}

variable "api_id" {
  type        = string
  description = "API Gateway HTTP API ID to map this domain to."
}

variable "stage_name" {
  type        = string
  description = "API Gateway stage to map this domain to."
}
