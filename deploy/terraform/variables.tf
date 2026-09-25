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
  default     = "ovh-sms"
}

# Image

variable "image_tag" {
  description = "Tag of the image pushed to the registry. Use immutable tags (version, commit SHA): changing it redeploys the container."
  type        = string
}

# Container

variable "max_scale" {
  description = "Maximum number of container instances. Keep it low to stay within the OVH sending rate."
  type        = number
  default     = 1
}

variable "memory_limit_bytes" {
  description = "Memory per instance. The vCPU is derived from it by Scaleway (128 MB = 70 mvCPU)."
  type        = number
  default     = 128 * 1000 * 1000
}

variable "container_timeout" {
  description = "Maximum processing time of a request, in seconds. Must be greater than ovh_timeout."
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

# OVH

variable "ovh_sms_account" {
  description = "OVH SMS account, e.g. sms-xx11111-1."
  type        = string
}

variable "ovh_sms_login" {
  description = "OVH SMS user."
  type        = string
}

variable "ovh_sms_password" {
  description = "OVH SMS user password."
  type        = string
  sensitive   = true
}

variable "ovh_sms_sender" {
  description = "Default sender, declared on the OVH SMS account."
  type        = string
}

variable "ovh_sms_no_stop" {
  description = "Remove the STOP mention (non-advertising SMS only)."
  type        = bool
  default     = false
}

variable "ovh_timeout" {
  description = "Timeout of the OVH call (Go duration)."
  type        = string
  default     = "10s"
}
