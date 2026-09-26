# Queues

resource "scaleway_mnq_sqs" "main" {}

# Used by Terraform to create the queues.
resource "scaleway_mnq_sqs_credentials" "admin" {
  project_id = scaleway_mnq_sqs.main.project_id
  name       = "${var.name}-terraform"

  permissions {
    can_manage  = true
    can_publish = false
    can_receive = false
  }
}

# Used by the trigger to read the queue.
resource "scaleway_mnq_sqs_credentials" "trigger" {
  project_id = scaleway_mnq_sqs.main.project_id
  name       = "${var.name}-trigger"

  permissions {
    can_manage  = false
    can_publish = false
    can_receive = true
  }
}

# Given to the applications that send SMS requests.
resource "scaleway_mnq_sqs_credentials" "producer" {
  project_id = scaleway_mnq_sqs.main.project_id
  name       = "${var.name}-producer"

  permissions {
    can_manage  = false
    can_publish = true
    can_receive = false
  }
}

resource "scaleway_mnq_sqs_queue" "dead_letter" {
  project_id      = scaleway_mnq_sqs.main.project_id
  name            = "${var.name}-dlq"
  sqs_endpoint    = scaleway_mnq_sqs.main.endpoint
  access_key      = scaleway_mnq_sqs_credentials.admin.access_key
  secret_key      = scaleway_mnq_sqs_credentials.admin.secret_key
  message_max_age = var.dlq_message_max_age
}

resource "scaleway_mnq_sqs_queue" "main" {
  project_id                 = scaleway_mnq_sqs.main.project_id
  name                       = var.name
  sqs_endpoint               = scaleway_mnq_sqs.main.endpoint
  access_key                 = scaleway_mnq_sqs_credentials.admin.access_key
  secret_key                 = scaleway_mnq_sqs_credentials.admin.secret_key
  message_max_age            = var.message_max_age
  visibility_timeout_seconds = var.visibility_timeout_seconds

  dead_letter_queue {
    id                = scaleway_mnq_sqs_queue.dead_letter.id
    max_receive_count = var.max_receive_count
  }
}

# Container

resource "scaleway_container_namespace" "main" {
  name = var.name
}

resource "scaleway_container" "main" {
  name         = var.name
  namespace_id = scaleway_container_namespace.main.id
  image        = "${var.image}:${var.image_tag}"
  port         = 8080

  # Only the trigger may call the container: a public one would let anyone
  # send SMS.
  privacy                = "private"
  https_connections_only = true

  memory_limit_bytes = var.memory_limit_bytes
  min_scale          = 0
  max_scale          = var.max_scale
  timeout            = var.container_timeout

  liveness_probe {
    http {
      path = "/healthz"
    }
    failure_threshold = 3
    interval          = "30s"
    timeout           = "5s"
  }

  # Only the settings of the selected protocol and provider are set: the
  # empty ones are left out.
  environment_variables = { for k, v in {
    SMS_PROTOCOL                 = var.sms_protocol
    SMS_PROVIDER                 = var.sms_provider
    SMS_SENDER                   = var.sms_sender
    SMS_API_TIMEOUT              = var.sms_api_timeout
    SMPP_ADDR                    = var.smpp_addr
    SMPP_TLS                     = tostring(var.smpp_tls)
    SMPP_SYSTEM_ID               = var.smpp_system_id
    SMPP_SYSTEM_TYPE             = var.smpp_system_type
    SMPP_SUBMIT_TIMEOUT          = var.smpp_submit_timeout
    TWILIO_ACCOUNT_SID           = var.twilio_account_sid
    TWILIO_MESSAGING_SERVICE_SID = var.twilio_messaging_service_sid
    OVH_SMS_ACCOUNT              = var.ovh_sms_account
    OVH_SMS_LOGIN                = var.ovh_sms_login
    OVH_SMS_NO_STOP              = tostring(var.ovh_sms_no_stop)
    CLICKSEND_USERNAME           = var.clicksend_username
    LOG_LEVEL                    = var.log_level
  } : k => v if v != "" }

  secret_environment_variables = { for k, v in {
    SMPP_PASSWORD     = var.smpp_password
    TWILIO_AUTH_TOKEN = var.twilio_auth_token
    OVH_SMS_PASSWORD  = var.ovh_sms_password
    CLICKSEND_API_KEY = var.clicksend_api_key
  } : k => v if v != "" }

  lifecycle {
    precondition {
      condition     = var.visibility_timeout_seconds > var.container_timeout
      error_message = "visibility_timeout_seconds must be greater than container_timeout, or a message may be delivered twice while still being processed."
    }
  }
}

# Trigger

resource "scaleway_container_trigger" "main" {
  container_id = scaleway_container.main.id
  name         = var.name

  destination_config {
    http_path   = "/"
    http_method = "post"
  }

  sqs {
    endpoint   = scaleway_mnq_sqs_queue.main.sqs_endpoint
    queue_url  = scaleway_mnq_sqs_queue.main.url
    access_key = scaleway_mnq_sqs_credentials.trigger.access_key
    secret_key = scaleway_mnq_sqs_credentials.trigger.secret_key
    region     = scaleway_mnq_sqs.main.region
  }
}
