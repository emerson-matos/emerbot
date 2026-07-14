variable "zone_id" {
  type        = string
  description = "Cloudflare Zone ID a resolver."

  validation {
    condition     = trimspace(var.zone_id) != ""
    error_message = "zone_id must be a non-empty Cloudflare zone identifier."
  }
}
