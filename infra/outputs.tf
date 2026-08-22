output "api_url" {
  description = "Public transport URL. Application endpoints still require Firebase auth and device signatures."
  value       = google_cloud_run_v2_service.api.uri
}
output "firebase_api_key" {
  description = "Public desktop API key restricted to Firebase Authentication endpoints."
  value       = google_apikeys_key.desktop.key_string
  sensitive   = true
}
output "transfer_bucket" {
  value = google_storage_bucket.transfers.name
}
output "artifact_repository" {
  value = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.backend.repository_id}"
}
output "workload_identity_provider" {
  value = google_iam_workload_identity_pool_provider.github.name
}
output "github_deploy_service_account" {
  value = google_service_account.github_deploy.email
}
output "google_oauth_secret_id" {
  description = "Secret Manager resource that holds the server-only Google OAuth client secret."
  value       = google_secret_manager_secret.google_oauth_client_secret.secret_id
}
