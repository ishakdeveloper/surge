locals {
  services = [
    "artifactregistry.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "sts.googleapis.com",
  ]
}

resource "google_project_service" "this" {
  for_each = toset(local.services)

  service            = each.value
  disable_on_destroy = false
}

# One registry for every image, in every environment. Tags are immutable:
# a git SHA names exactly one build, and pushing a different image under an
# existing tag is refused rather than silently repointing what was deployed.
resource "google_artifact_registry_repository" "images" {
  repository_id = "images"
  location      = var.region
  format        = "DOCKER"
  description   = "Every surge image, pushed from main and deployed by digest."

  docker_config {
    immutable_tags = true
  }

  # Deployed images are tagged with the SHA they were built from, so they
  # stay. What goes is what nothing names: layers of a build that was pushed
  # again, and manifests left behind by a failed push.
  cleanup_policies {
    id     = "untagged"
    action = "DELETE"
    condition {
      tag_state  = "UNTAGGED"
      older_than = "604800s"
    }
  }

  depends_on = [google_project_service.this]
}

# GitHub's OIDC token, exchanged for a short-lived Google credential. No
# service-account key exists anywhere, so there is none to leak from a GitHub
# secret or to rotate.
resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"

  depends_on = [google_project_service.this]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "GitHub Actions OIDC"

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }

  # Evaluated before any binding: a token from any other repository is refused
  # outright, whatever the bindings say.
  attribute_condition = "assertion.repository == \"${var.github_repository}\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# Pushes images and does nothing else.
resource "google_service_account" "image_pusher" {
  account_id   = "image-pusher"
  display_name = "CI image pusher"
  description  = "Impersonated by GitHub Actions on ${var.push_ref} to push to the images registry."
}

resource "google_artifact_registry_repository_iam_member" "image_pusher" {
  location   = google_artifact_registry_repository.images.location
  repository = google_artifact_registry_repository.images.repository_id
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.image_pusher.email}"
}

# Only workflows running on the push ref may become the pusher. A pull request,
# a branch, or a fork's workflow authenticates to nothing.
resource "google_service_account_iam_member" "image_pusher_from_github" {
  service_account_id = google_service_account.image_pusher.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.ref/${var.push_ref}"
}
