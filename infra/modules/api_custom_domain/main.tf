# ACM cert (DNS-validated) + API Gateway custom domain + mapping for a single
# hostname. Validation is a two-step chain to avoid a dependency cycle: the
# validation CNAME must exist in Cloudflare before aws_acm_certificate_validation
# resolves, and the CNAME's target isn't known until after the cert exists.
resource "aws_acm_certificate" "this" {
  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

locals {
  # ACM issues one validation record per SAN; a single-domain cert has one.
  validation_option = tolist(aws_acm_certificate.this.domain_validation_options)[0]
}

resource "cloudflare_dns_record" "cert_validation" {
  zone_id = var.zone_id
  name    = trimsuffix(trimsuffix(local.validation_option.resource_record_name, "."), ".${var.zone_name}")
  type    = local.validation_option.resource_record_type
  content = trimsuffix(local.validation_option.resource_record_value, ".")
  ttl     = 300
  proxied = false # must resolve as a plain CNAME for ACM to see it
  comment = "ACM DNS validation for ${var.domain_name}"

  lifecycle {
    ignore_changes = [content]
  }
}

resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [trimsuffix(local.validation_option.resource_record_name, ".")]

  depends_on = [cloudflare_dns_record.cert_validation]
}

resource "aws_apigatewayv2_domain_name" "this" {
  domain_name = var.domain_name

  domain_name_configuration {
    certificate_arn = aws_acm_certificate_validation.this.certificate_arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }
}

resource "aws_apigatewayv2_api_mapping" "this" {
  api_id      = var.api_id
  domain_name = aws_apigatewayv2_domain_name.this.id
  stage       = var.stage_name
}
