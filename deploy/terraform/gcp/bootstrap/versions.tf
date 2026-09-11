# The first slice of the cloud: somewhere to push images, and a way for CI to
# push there without a key. Everything else — the network, GKE, Cloud SQL —
# is Phase 5, and is applied from CI with the identity this creates.
#
# State lives in a GCS bucket that has to exist before `init`, and that this
# configuration therefore cannot create. Once, by hand:
#
#   gcloud storage buckets create gs://<project>-tofu-state \
#     --project <project> --location europe-west4 --uniform-bucket-level-access
#   gcloud storage buckets update gs://<project>-tofu-state --versioning
#
#   tofu init -backend-config="bucket=<project>-tofu-state"
#   tofu apply -var project_id=<project>
terraform {
  required_version = ">= 1.10"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.2"
    }
  }

  backend "gcs" {
    prefix = "bootstrap"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}
