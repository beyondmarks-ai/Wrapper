variable "project_id" {
  description = "Google Cloud project that also owns the Firebase project."
  type        = string
}
variable "region" {
  description = "Cloud Run, Artifact Registry, and Cloud Tasks region."
  type        = string
  default     = "us-central1"
}
variable "firestore_location" {
  description = "Firestore database location. Use the existing Firebase database location."
  type        = string
  default     = "nam5"
}
variable "container_image" {
  description = "Initial Cloud Run image. CI replaces it with an immutable Wrapper image."
  type        = string
  default     = "us-docker.pkg.dev/cloudrun/container/hello@sha256:9a0e9a5c7a19281e7617991d2fc61809de4973e6e75a10b2f07df3719ffda33c"
}
variable "github_repository" {
  description = "GitHub repository allowed to deploy, in owner/name form."
  type        = string
  default     = "beyondmarks-ai/Wrapper"
}
variable "billing_account" {
  description = "Optional billing account ID used to create a monthly budget."
  type        = string
  default     = ""
}
variable "monthly_budget_amount" {
  description = "Monthly project budget amount in the billing account currency when billing_account is set."
  type        = number
  default     = 25
}
variable "google_client_id" {
  description = "Public Google Desktop OAuth client ID used by Wrapper."
  type        = string
  default     = ""
  validation {
    condition     = var.google_client_id == "" || can(regex("^[0-9]+-[a-z0-9]+[.]apps[.]googleusercontent[.]com$", var.google_client_id))
    error_message = "google_client_id must be a Google Desktop OAuth client ID."
  }
}
