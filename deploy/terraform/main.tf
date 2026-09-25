# Registry

resource "scaleway_registry_namespace" "main" {
  name      = var.name
  is_public = false
}

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
  image        = "${scaleway_registry_namespace.main.endpoint}/ovh-sms-messaging:${var.image_tag}"
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

  environment_variables = {
    OVH_SMS_ACCOUNT = var.ovh_sms_account
    OVH_SMS_LOGIN   = var.ovh_sms_login
    OVH_SMS_SENDER  = var.ovh_sms_sender
    OVH_SMS_NO_STOP = tostring(var.ovh_sms_no_stop)
    OVH_TIMEOUT     = var.ovh_timeout
    LOG_LEVEL       = var.log_level
  }

  secret_environment_variables = {
    OVH_SMS_PASSWORD = var.ovh_sms_password
  }

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
