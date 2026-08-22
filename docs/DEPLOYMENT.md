# Production deployment

This runbook deploys Wrapper Cloud to the same Google Cloud project used by Firebase Authentication. Use separate projects for development and production.

## 1. Prerequisites

Install Google Cloud CLI, Terraform, and GitHub CLI. Enable billing and authenticate without creating service-account keys:

```powershell
gcloud auth login
gcloud auth application-default login
gh auth login
```

The deploying account needs permission to enable APIs, create the resources in `infra/`, and administer repository Actions variables. Confirm the target project and billing account before continuing.

## 2. Configure Firebase Authentication first

In Firebase Console:

1. Open Authentication and enable the Google sign-in provider.
2. Configure the OAuth consent screen and test users while the app is in testing mode.
3. Create an OAuth 2.0 client of type **Desktop app** in the same Google Cloud project.
4. Download its client JSON to a protected local path. Never commit or upload that file to GitHub.

Wrapper uses Authorization Code with PKCE and a loopback callback. The desktop sends the short-lived authorization code, verifier, and exact loopback URI to Wrapper Cloud over HTTPS. Cloud Run reads the OAuth client secret from Secret Manager, exchanges the code with Google, and returns only the resulting ID token. The secret is never embedded in the installer, stored in Terraform state, or written to GitHub variables.

## 3. Provision production infrastructure

Firestore location cannot be changed after database creation. Review `Region` and `FirestoreLocation`, then run:

```powershell
.\scripts\deploy-production.ps1 `
  -ProjectId YOUR_PROJECT_ID `
  -Region us-central1 `
  -FirestoreLocation nam5 `
  -GoogleClientId 'YOUR_DESKTOP_CLIENT_ID' `
  -GoogleClientSecretFile 'C:\secure\client_secret.json' `
  -Apply `
  -ConfigureGitHub
```

A Desktop OAuth client ID is required for production. On the first run, pass Google's downloaded JSON with `-GoogleClientSecretFile`; after an enabled Secret Manager version exists, later runs do not need the file. Securely remove or archive the downloaded JSON afterward. To create the optional billing budget, add `-BillingAccount '000000-000000-000000' -MonthlyBudgetAmount 2000`.

The script is write-protected unless `-Apply` is supplied and presents a high-impact confirmation. It:

- verifies gcloud authentication, billing, and Firebase Google sign-in;
- creates a versioned, private `gs://YOUR_PROJECT_ID-wrapper-tfstate` state bucket;
- initializes the partial GCS backend and applies an exact saved Terraform plan;
- creates a dedicated desktop API key restricted to Identity Toolkit and Secure Token;
- provisions Firestore with PITR, delete protection, TTL, and the required event index;
- provisions the private transfer bucket with soft delete disabled, Cloud Tasks, Artifact Registry, Cloud Run, least-privilege service accounts, and GitHub Workload Identity Federation;
- stores the Google OAuth client secret only as a Secret Manager version and grants access only to the Cloud Run runtime identity;
- writes only public application/deployment configuration to GitHub Actions variables.

No service-account JSON key, OAuth client secret, Terraform state, or API key is written to the repository. Secret data is loaded directly into Secret Manager and is not managed as a Terraform secret version. The Firebase desktop API key is embedded in release binaries by design, but it is restricted to authentication endpoints.

For a manual Terraform workflow, create and secure the state bucket first, then use:

```powershell
terraform -chdir=infra init -reconfigure `
  -backend-config='bucket=YOUR_PROJECT_ID-wrapper-tfstate' `
  -backend-config='prefix=wrapper/production'
terraform -chdir=infra apply -target=google_secret_manager_secret.google_oauth_client_secret -var='project_id=YOUR_PROJECT_ID' -var='google_client_id=YOUR_DESKTOP_CLIENT_ID'
gcloud secrets versions add wrapper-google-oauth-client-secret --project=YOUR_PROJECT_ID --data-file=C:\secure\client-secret-only.txt
terraform -chdir=infra plan -out=wrapper-production.tfplan -var='project_id=YOUR_PROJECT_ID' -var='google_client_id=YOUR_DESKTOP_CLIENT_ID'
terraform -chdir=infra apply wrapper-production.tfplan
```

If Firebase already owns the default Firestore database, import it before applying:

```powershell
terraform -chdir=infra import google_firestore_database.default "projects/YOUR_PROJECT_ID/databases/(default)"
```

## 4. Deploy the API

The initial Cloud Run revision uses Google's hello container so infrastructure can be created before an application image exists. After GitHub variables are configured, pushing the amended `main` commit starts `.github/workflows/deploy-backend.yml`. It authenticates through Workload Identity Federation, pushes an immutable commit-SHA image, deploys it, and verifies `/health`.

Required repository variable names are:

```text
GCP_PROJECT_ID
GCP_REGION
GCP_WIF_PROVIDER
GCP_DEPLOY_SERVICE_ACCOUNT
WRAPPER_CLOUD_API_URL
WRAPPER_GOOGLE_CLIENT_ID
WRAPPER_FIREBASE_API_KEY
```

## 5. Enable a beta user

Sign in once, find the Firebase UID, then create:

```text
betaInvites/{firebase_uid}
  enabled: true
```

The API denies authenticated users without an enabled invite document.

## 6. Build the Windows release

```powershell
$env:WRAPPER_CLOUD_API_URL = terraform -chdir=infra output -raw api_url
$env:WRAPPER_GOOGLE_CLIENT_ID = 'YOUR_DESKTOP_CLIENT_ID'
$env:WRAPPER_FIREBASE_API_KEY = terraform -chdir=infra output -raw firebase_api_key
.\scripts\package.ps1 -EverythingDll C:\sdk\dll\Everything64.dll
```

For signed releases, pass `-CertificateThumbprint` and install Windows SDK `signtool.exe`. The release workflow downloads the official Everything SDK, builds the installer, and publishes checksums.

## 7. Production smoke test

```powershell
Invoke-WebRequest "$(terraform -chdir=infra output -raw api_url)/health"
.\bin\wrap.exe auth login
.\bin\wrap.exe device register --name $env:COMPUTERNAME
.\bin\wrap.exe agent status
```

Use a second Windows user or VM to prove pairing, remote Everything search, file and folder transfers, resumed download, destination hashes, device revocation, quota rejection, and ciphertext deletion. Do not claim a two-device production test from a one-machine local run.

## Operations

- Monitor Cloud Run 4xx/5xx rates, latency, instance count, Firestore operations, queue failures, rejected quotas, and bucket bytes.
- Alert on budget thresholds and Cloud Tasks retry exhaustion.
- Revoke a lost PC with `wrap device revoke DEVICE_ID`.
- Roll back Cloud Run to a prior immutable revision; protocol version 1 rejects incompatible envelopes.
- Storage deletion is scheduled at 24 hours and reinforced by the bucket lifecycle. GCS lifecycle timing is asynchronous, not exact-second deletion.
- Keep the Terraform state bucket versioned, access-controlled, and separate from transfer ciphertext.
