output "registry_endpoint" {
  description = "Registry to push the image to: <registry_endpoint>/ovh-sms-messaging:<image_tag>."
  value       = scaleway_registry_namespace.main.endpoint
}

output "sqs_endpoint" {
  description = "SQS endpoint used by producers."
  value       = scaleway_mnq_sqs.main.endpoint
}

output "queue_url" {
  description = "URL of the queue receiving SMS requests."
  value       = scaleway_mnq_sqs_queue.main.url
}

output "dead_letter_queue_url" {
  description = "URL of the dead letter queue."
  value       = scaleway_mnq_sqs_queue.dead_letter.url
}

output "producer_access_key" {
  description = "SQS access key allowed to publish to the queue."
  value       = scaleway_mnq_sqs_credentials.producer.access_key
  sensitive   = true
}

output "producer_secret_key" {
  description = "SQS secret key allowed to publish to the queue."
  value       = scaleway_mnq_sqs_credentials.producer.secret_key
  sensitive   = true
}
