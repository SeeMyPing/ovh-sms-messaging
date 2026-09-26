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
  description = "Maximum number of container instances. With SMPP, each one opens its own bind: stay within the number of binds allowed by the provider. Over HTTP, stay within the provider's rate limit."
  type        = number
  default     = 1
}

variable "memory_limit_bytes" {
  description = "Memory per instance. The vCPU is derived from it by Scaleway (128 MB = 70 mvCPU)."
  type        = number
  default     = 128 * 1000 * 1000
}

variable "container_timeout" {
  description = "Maximum processing time of a request, in seconds. Must leave room for the SMPP bind and every part of a long message, or for the HTTP API call."
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

# SMS provider

variable "sms_protocol" {
  description = "Sending protocol: smpp (any SMSC) or http (provider API)."
  type        = string
  default     = "smpp"

  validation {
    condition     = contains(["smpp", "http"], var.sms_protocol)
    error_message = "sms_protocol must be smpp or http."
  }
}

variable "sms_provider" {
  description = "With sms_protocol = http: twilio, ovh or clicksend. With smpp: optional, only shown in the logs."
  type        = string
  default     = ""

  validation {
    condition     = var.sms_protocol != "http" || contains(["twilio", "ovh", "clicksend"], var.sms_provider)
    error_message = "With sms_protocol = http, sms_provider must be twilio, ovh or clicksend."
  }
}

variable "sms_sender" {
  description = "Default sender: alphanumeric (11 characters max), short code or +international number. Required with SMPP and OVH, and with Twilio unless twilio_messaging_service_sid is set."
  type        = string
  default     = ""
}

variable "sms_api_timeout" {
  description = "Timeout of each HTTP API call (Go duration)."
  type        = string
  default     = "10s"
}

# SMPP

variable "smpp_addr" {
  description = "SMSC address, host:port."
  type        = string
  default     = ""
}

variable "smpp_tls" {
  description = "Connect to the SMSC over TLS."
  type        = bool
  default     = false
}

variable "smpp_system_id" {
  description = "SMPP system_id (login)."
  type        = string
  default     = ""
}

variable "smpp_password" {
  description = "SMPP password."
  type        = string
  default     = ""
  sensitive   = true
}

variable "smpp_system_type" {
  description = "SMPP system_type, if the provider requires one."
  type        = string
  default     = ""
}

variable "smpp_submit_timeout" {
  description = "Wait for each submit_sm_resp (Go duration)."
  type        = string
  default     = "10s"
}

# Twilio

variable "twilio_account_sid" {
  description = "Twilio Account SID (AC...)."
  type        = string
  default     = ""
}

variable "twilio_auth_token" {
  description = "Twilio Auth Token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "twilio_messaging_service_sid" {
  description = "Twilio Messaging Service SID (MG...), used when no sender is given."
  type        = string
  default     = ""
}

# OVH (http2sms)

variable "ovh_sms_account" {
  description = "OVH SMS account, e.g. sms-xx11111-1."
  type        = string
  default     = ""
}

variable "ovh_sms_login" {
  description = "SMS user of the OVH account (not the NIC handle)."
  type        = string
  default     = ""
}

variable "ovh_sms_password" {
  description = "Password of the OVH SMS user."
  type        = string
  default     = ""
  sensitive   = true
}

variable "ovh_sms_no_stop" {
  description = "Remove the STOP mention, for non-advertising messages."
  type        = bool
  default     = false
}

# ClickSend

variable "clicksend_username" {
  description = "ClickSend API username."
  type        = string
  default     = ""
}

variable "clicksend_api_key" {
  description = "ClickSend API key."
  type        = string
  default     = ""
  sensitive   = true
}
