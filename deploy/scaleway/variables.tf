variable "project_id" {
  description = "Scaleway project ID. Defaults to the provider's project (SCW_DEFAULT_PROJECT_ID)."
  type        = string
  default     = null
}

variable "region" {
  description = "Scaleway region."
  type        = string
  default     = "fr-par"
}

variable "name" {
  description = "Prefix used to name every resource."
  type        = string
  default     = "sqs-to-smpp"
}

# Image

variable "image" {
  description = "Container image, without tag. Published by the CI on every merge to main."
  type        = string
  default     = "ghcr.io/seemyping/sqs-to-smpp-gateway"
}

variable "image_tag" {
  description = "Image tag to deploy: sha-<commit> or a release version. Use immutable tags: changing it redeploys the container, reusing one does not."
  type        = string
}

# Container

variable "max_scale" {
  description = "Maximum number of container instances. Each one opens its own SMPP bind: stay within the number of binds allowed by the provider."
  type        = number
  default     = 1
}

variable "memory_limit_bytes" {
  description = "Memory per instance. The vCPU is derived from it by Scaleway (128 MB = 70 mvCPU)."
  type        = number
  default     = 128 * 1000 * 1000
}

variable "container_timeout" {
  description = "Maximum processing time of a request, in seconds. Must leave room for the SMPP bind and every part of a long message."
  type        = number
  default     = 30
}

variable "log_level" {
  description = "Application log level: debug, info, warn, error."
  type        = string
  default     = "info"
}

# Queues

variable "visibility_timeout_seconds" {
  description = "Time a message stays hidden once received. Must be greater than container_timeout."
  type        = number
  default     = 60
}

variable "message_max_age" {
  description = "Retention of the main queue, in seconds."
  type        = number
  default     = 4 * 24 * 3600
}

variable "dlq_message_max_age" {
  description = "Retention of the dead letter queue, in seconds (maximum 14 days)."
  type        = number
  default     = 14 * 24 * 3600
}

variable "max_receive_count" {
  description = "Deliveries of a message before it is moved to the dead letter queue."
  type        = number
  default     = 4
}

# SMPP

variable "smpp_addr" {
  description = "SMSC address, host:port."
  type        = string
}

variable "smpp_tls" {
  description = "Connect to the SMSC over TLS."
  type        = bool
  default     = false
}

variable "smpp_system_id" {
  description = "SMPP system_id (login)."
  type        = string
}

variable "smpp_password" {
  description = "SMPP password."
  type        = string
  sensitive   = true
}

variable "smpp_system_type" {
  description = "SMPP system_type, if the provider requires one."
  type        = string
  default     = ""
}

variable "smpp_source_addr" {
  description = "Default sender: alphanumeric (11 characters max), short code or +international number."
  type        = string
}

variable "smpp_submit_timeout" {
  description = "Wait for each submit_sm_resp (Go duration)."
  type        = string
  default     = "10s"
}
