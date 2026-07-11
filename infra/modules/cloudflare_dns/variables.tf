variable "zone_id" {
  type = string

  validation {
    condition     = length(var.records) == 0 || trimspace(var.zone_id) != ""
    error_message = "zone_id must be set when records are configured."
  }
}

variable "records" {
  type = list(object({
    id      = string
    name    = string
    type    = string
    content = string
    ttl     = optional(number, 1)
    proxied = optional(bool, false)
    comment = optional(string)
  }))
  default = []

  validation {
    condition = length(var.records) == length(distinct([
      for record in var.records : trimspace(record.id)
    ]))
    error_message = "Cloudflare DNS record ids must be unique."
  }

  validation {
    condition = alltrue([
      for record in var.records :
      trimspace(record.id) != "" &&
      trimspace(record.name) != "" &&
      trimspace(record.type) != "" &&
      trimspace(record.content) != ""
    ])
    error_message = "Cloudflare DNS records must define non-empty id, name, type, and content."
  }
}

