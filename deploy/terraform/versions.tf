terraform {
  required_version = ">= 1.5"

  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "~> 2.83"
    }
  }
}

# Credentials come from the environment (SCW_ACCESS_KEY, SCW_SECRET_KEY,
# SCW_DEFAULT_PROJECT_ID) or the Scaleway CLI config.
provider "scaleway" {
  region     = var.region
  project_id = var.project_id
}
