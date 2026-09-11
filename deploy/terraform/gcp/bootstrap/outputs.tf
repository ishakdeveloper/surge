# The three values the workflow needs, set as repository variables (not
# secrets: none of them grants anything on its own):
#
#   gh variable set REGISTRY         --body "$(tofu output -raw registry)"
#   gh variable set GCP_WIF_PROVIDER --body "$(tofu output -raw workload_identity_provider)"
#   gh variable set GCP_PUSH_SA      --body "$(tofu output -raw image_pusher)"
#
# Until REGISTRY is set, CI builds and scans and pushes nothing.

output "registry" {
  description = "Where images are pushed, without a trailing slash."
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"
}

output "workload_identity_provider" {
  description = "The provider google-github-actions/auth exchanges GitHub's token with."
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "image_pusher" {
  description = "The service account CI impersonates to push."
  value       = google_service_account.image_pusher.email
}
