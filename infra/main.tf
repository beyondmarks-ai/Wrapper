locals {
  services = toset([
    "apikeys.googleapis.com",
    "artifactregistry.googleapis.com",
    "billingbudgets.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "cloudtasks.googleapis.com",
    "firestore.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "identitytoolkit.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "serviceusage.googleapis.com",
    "storage.googleapis.com",
    "sts.googleapis.com",
  ])
}

resource "google_apikeys_key" "desktop" {
  project      = var.project_id
  name         = "wrapper-desktop"
  display_name = "Wrapper desktop authentication"

  restrictions {
    api_targets {
      service = "identitytoolkit.googleapis.com"
    }
    api_targets {
      service = "securetoken.googleapis.com"
    }
  }

  deletion_policy = "PREVENT"
  depends_on      = [google_project_service.required]
}
data "google_project" "current" {
  project_id = var.project_id
}

resource "google_project_service" "required" {
  for_each           = local.services
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_secret_manager_secret" "google_oauth_client_secret" {
  project             = var.project_id
  secret_id           = "wrapper-google-oauth-client-secret"
  deletion_protection = true
  labels              = { application = "wrapper" }
  replication {
    auto {}
  }

  depends_on = [google_project_service.required]
}

resource "random_id" "bucket" {
  byte_length = 4
}

resource "google_storage_bucket" "transfers" {
  name                        = "${var.project_id}-wrapper-transfers-${random_id.bucket.hex}"
  location                    = var.region
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = false

  soft_delete_policy {
    retention_duration_seconds = 0
  }

  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.required]
}

resource "google_firestore_database" "default" {
  project                           = var.project_id
  name                              = "(default)"
  location_id                       = var.firestore_location
  type                              = "FIRESTORE_NATIVE"
  deletion_policy                   = "ABANDON"
  delete_protection_state           = "DELETE_PROTECTION_ENABLED"
  point_in_time_recovery_enablement = "POINT_IN_TIME_RECOVERY_ENABLED"

  depends_on = [google_project_service.required]
}

resource "google_firestore_field" "ttl" {
  for_each   = toset(["events", "pairingCodes", "requestNonces", "transfers"])
  project    = var.project_id
  database   = google_firestore_database.default.name
  collection = each.value
  field      = "expiresAt"
  ttl_config {}
}

resource "google_firestore_index" "events_target_expiry" {
  project     = var.project_id
  database    = google_firestore_database.default.name
  collection  = "events"
  query_scope = "COLLECTION"

  fields {
    field_path = "targetDevice"
    order      = "ASCENDING"
  }
  fields {
    field_path = "expiresAt"
    order      = "ASCENDING"
  }
}

resource "google_artifact_registry_repository" "backend" {
  project       = var.project_id
  location      = var.region
  repository_id = "wrapper"
  description   = "Wrapper Cloud container images"
  format        = "DOCKER"

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }

  depends_on = [google_project_service.required]
}

resource "google_cloud_tasks_queue" "expiration" {
  project  = var.project_id
  location = var.region
  name     = "wrapper-expiration"

  rate_limits {
    max_concurrent_dispatches = 20
    max_dispatches_per_second = 10
  }
  retry_config {
    max_attempts       = 5
    max_retry_duration = "3600s"
    min_backoff        = "5s"
    max_backoff        = "300s"
    max_doublings      = 5
  }
  depends_on = [google_project_service.required]
}

resource "google_service_account" "runtime" {
  project      = var.project_id
  account_id   = "wrapper-cloud"
  display_name = "Wrapper Cloud runtime"

  depends_on = [google_project_service.required]
}

resource "google_project_iam_member" "runtime_datastore" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_project_iam_member" "runtime_firebase_auth" {
  project = var.project_id
  role    = "roles/firebaseauth.viewer"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_storage_bucket_iam_member" "runtime_objects" {
  bucket = google_storage_bucket.transfers.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_project_iam_member" "runtime_tasks" {
  project = var.project_id
  role    = "roles/cloudtasks.enqueuer"
  member  = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_secret_manager_secret_iam_member" "runtime_google_oauth_secret" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.google_oauth_client_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_service_account_iam_member" "runtime_signs_urls" {
  service_account_id = google_service_account.runtime.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_cloud_run_v2_service" "api" {
  project             = var.project_id
  name                = "wrapper-cloud"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  # Terraform creates the service; GitHub Actions owns immutable application
  # image revisions after bootstrap. Do not roll a deployed image backward.
  lifecycle {
    ignore_changes = [client, client_version, template[0].containers[0].image]
  }

  scaling {
    min_instance_count = 0
    max_instance_count = 10
  }

  template {
    service_account                  = google_service_account.runtime.email
    timeout                          = "45s"
    max_instance_request_concurrency = 40

    containers {
      image = var.container_image
      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        cpu_idle = true
      }
      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }
      env {
        name  = "WRAPPER_TRANSFER_BUCKET"
        value = google_storage_bucket.transfers.name
      }
      env {
        name  = "WRAPPER_TASK_LOCATION"
        value = var.region
      }
      env {
        name  = "WRAPPER_TASK_QUEUE"
        value = google_cloud_tasks_queue.expiration.name
      }
      env {
        name  = "WRAPPER_GOOGLE_CLIENT_ID"
        value = var.google_client_id
      }
      env {
        name = "WRAPPER_GOOGLE_CLIENT_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.google_oauth_client_secret.secret_id
            version = "latest"
          }
        }
      }
      startup_probe {
        initial_delay_seconds = 1
        timeout_seconds       = 2
        period_seconds        = 5
        failure_threshold     = 12
        http_get {
          path = "/"
          port = 8080
        }
      }
      liveness_probe {
        timeout_seconds   = 2
        period_seconds    = 30
        failure_threshold = 3
        http_get {
          path = "/"
          port = 8080
        }
      }
    }
  }

  depends_on = [
    google_project_service.required,
    google_firestore_database.default,
    google_project_iam_member.runtime_datastore,
    google_project_iam_member.runtime_firebase_auth,
    google_storage_bucket_iam_member.runtime_objects,
    google_project_iam_member.runtime_tasks,
    google_secret_manager_secret_iam_member.runtime_google_oauth_secret,
    google_service_account_iam_member.runtime_signs_urls,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "public_transport" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_iam_workload_identity_pool" "github" {
  project                   = var.project_id
  workload_identity_pool_id = "github-wrapper"
  display_name              = "GitHub Wrapper deployments"

  depends_on = [google_project_service.required]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = var.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "Wrapper GitHub Actions"
  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
  }
  attribute_condition = "assertion.repository == '${var.github_repository}'"
  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account" "github_deploy" {
  project      = var.project_id
  account_id   = "wrapper-github-deploy"
  display_name = "Wrapper GitHub deployment"

  depends_on = [google_project_service.required]
}

resource "google_service_account_iam_member" "github_wif" {
  service_account_id = google_service_account.github_deploy.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repository}"
}

resource "google_project_iam_member" "github_artifacts" {
  project = var.project_id
  role    = "roles/artifactregistry.writer"
  member  = "serviceAccount:${google_service_account.github_deploy.email}"
}

resource "google_project_iam_member" "github_run" {
  project = var.project_id
  role    = "roles/run.developer"
  member  = "serviceAccount:${google_service_account.github_deploy.email}"
}

resource "google_service_account_iam_member" "github_runtime_user" {
  service_account_id = google_service_account.runtime.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.github_deploy.email}"
}

resource "google_billing_budget" "monthly" {
  count           = var.billing_account == "" ? 0 : 1
  billing_account = trimprefix(var.billing_account, "billingAccounts/")
  display_name    = "Wrapper monthly budget"
  budget_filter {
    projects = ["projects/${data.google_project.current.number}"]
  }
  amount {
    specified_amount {
      units = tostring(var.monthly_budget_amount)
    }
  }
  threshold_rules {
    threshold_percent = 0.5
  }
  threshold_rules {
    threshold_percent = 0.9
  }
  threshold_rules {
    threshold_percent = 1.0
  }

  depends_on = [google_project_service.required]
}
