[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[a-z][a-z0-9-]{4,28}[a-z0-9]$')]
    [string]$ProjectId,

    [ValidatePattern('^$|^[0-9]+-[a-zA-Z0-9_-]+\.apps\.googleusercontent\.com$')]
    [string]$GoogleClientId = '',

    [string]$GoogleClientSecretFile = '',

    [ValidatePattern('^[a-z]+-[a-z]+[0-9]+$')]
    [string]$Region = 'us-central1',

    [ValidatePattern('^[a-z0-9-]+$')]
    [string]$FirestoreLocation = 'nam5',

    [ValidatePattern('^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$')]
    [string]$GitHubRepository = 'beyondmarks-ai/Wrapper',

    [ValidatePattern('^$|^(billingAccounts/)?[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$')]
    [string]$BillingAccount = '',

    [ValidateRange(1, 1000000)]
    [decimal]$MonthlyBudgetAmount = 25,

    [string]$StateBucket,

    [switch]$Apply,
    [switch]$ConfigureGitHub
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$env:CLOUDSDK_CORE_DISABLE_FILE_LOGGING = 'true'

function Require-Command {
    param([Parameter(Mandatory)][string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "$Name is required but was not found on PATH."
    }
}

function Invoke-Native {
    param(
        [Parameter(Mandatory)][string]$File,
        [Parameter(Mandatory)][string[]]$Arguments
    )
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$File failed with exit code $LASTEXITCODE."
    }
}

function Invoke-Capture {
    param(
        [Parameter(Mandatory)][string]$File,
        [Parameter(Mandatory)][string[]]$Arguments
    )
    $output = & $File @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$File failed with exit code $LASTEXITCODE."
    }
    return $output
}

function Test-FirebaseGoogleProvider {
    param([Parameter(Mandatory)][string]$TargetProject)

    $keys = (Invoke-Capture 'gcloud' @(
        'services', 'api-keys', 'list',
        "--project=$TargetProject",
        '--format=json'
    ) | Out-String) | ConvertFrom-Json
    $firebaseKey = @($keys | Where-Object {
        $_.displayName -like '*Firebase*' -or $_.displayName -like '*Browser key*'
    }) | Select-Object -First 1
    if ($null -eq $firebaseKey) {
        throw 'Firebase has no bootstrap Web API key. Enable Firebase Authentication and Google sign-in first.'
    }

    $keyString = (Invoke-Capture 'gcloud' @(
        'services', 'api-keys', 'get-key-string', [string]$firebaseKey.name,
        "--project=$TargetProject",
        '--format=value(keyString)'
    ) | Out-String).Trim()
    if ([string]::IsNullOrWhiteSpace($keyString)) {
        throw 'Firebase API key retrieval returned an empty value.'
    }

    $endpoint = 'https://identitytoolkit.googleapis.com/v1/accounts:signInWithIdp?key=' +
        [Uri]::EscapeDataString($keyString)
    $body = @{
        postBody           = 'id_token=invalid-wrapper-deployment-preflight&providerId=google.com'
        requestUri         = 'http://localhost'
        returnIdpCredential = $true
        returnSecureToken  = $true
    } | ConvertTo-Json -Compress

    $message = ''
    try {
        Invoke-RestMethod -Uri $endpoint -Method Post -ContentType 'application/json' -Body $body | Out-Null
        throw 'Firebase unexpectedly accepted the deployment preflight token.'
    } catch {
        if ($null -eq $_.Exception.Response) {
            throw
        }
        $reader = New-Object IO.StreamReader($_.Exception.Response.GetResponseStream())
        try {
            $failure = $reader.ReadToEnd() | ConvertFrom-Json
            $message = [string]$failure.error.message
        } finally {
            $reader.Dispose()
        }
    } finally {
        Remove-Variable keyString -ErrorAction SilentlyContinue
    }

    if (-not $message.StartsWith('INVALID_IDP_RESPONSE', [StringComparison]::Ordinal)) {
        throw "Firebase Google sign-in is not ready ($message). Enable Google in Firebase Authentication and allow the Desktop OAuth client ID."
    }
}

if (-not $Apply) {
    throw 'Deployment is intentionally write-protected. Review the plan, then rerun with -Apply.'
}

foreach ($tool in @('gcloud', 'terraform')) {
    Require-Command $tool
}
if ([string]::IsNullOrWhiteSpace($GoogleClientId)) {
    throw 'GoogleClientId is required for the server-mediated production OAuth flow.'
}
if ($ConfigureGitHub) {
    Require-Command 'gh'
}
if ($GoogleClientSecretFile -and -not (Test-Path -LiteralPath $GoogleClientSecretFile -PathType Leaf)) {
    throw 'GoogleClientSecretFile does not exist or is not a file.'
}
if ([string]::IsNullOrWhiteSpace($StateBucket)) {
    $StateBucket = "$ProjectId-wrapper-tfstate"
}
if ($StateBucket -notmatch '^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$') {
    throw 'StateBucket is not a valid Cloud Storage bucket name.'
}

$account = (Invoke-Capture 'gcloud' @('config', 'get-value', 'account') | Out-String).Trim()
if ([string]::IsNullOrWhiteSpace($account)) {
    throw 'No active gcloud account. Run gcloud auth login.'
}
$billing = (Invoke-Capture 'gcloud' @(
    'billing', 'projects', 'describe', $ProjectId,
    '--format=value(billingEnabled)'
) | Out-String).Trim()
if ($billing -ne 'True') {
    throw "Billing is not enabled for $ProjectId."
}

Test-FirebaseGoogleProvider -TargetProject $ProjectId

$summary = "Google Cloud project $ProjectId in $Region, Firestore location $FirestoreLocation, state bucket gs://$StateBucket"
if (-not $PSCmdlet.ShouldProcess($summary, 'Provision Wrapper production infrastructure and configure deployment outputs')) {
    return
}

$bucketUri = "gs://$StateBucket"
$existingBucket = (Invoke-Capture 'gcloud' @(
    'storage', 'buckets', 'list',
    "--project=$ProjectId",
    "--filter=name=$StateBucket",
    '--format=value(name)'
) | Out-String).Trim()
if ([string]::IsNullOrWhiteSpace($existingBucket)) {
    Invoke-Native 'gcloud' @(
        'storage', 'buckets', 'create', $bucketUri,
        "--project=$ProjectId",
        "--location=$Region",
        '--uniform-bucket-level-access',
        '--public-access-prevention'
    )
}
Invoke-Native 'gcloud' @(
    'storage', 'buckets', 'update', $bucketUri,
    '--versioning',
    '--uniform-bucket-level-access',
    '--public-access-prevention'
)

$root = Split-Path -Parent $PSScriptRoot
$infra = Join-Path $root 'infra'
$planPath = Join-Path $infra 'wrapper-production.tfplan'
$env:TF_IN_AUTOMATION = 'true'

Invoke-Native 'terraform' @(
    "-chdir=$infra", 'init', '-reconfigure', '-input=false',
    "-backend-config=bucket=$StateBucket",
    '-backend-config=prefix=wrapper/production'
)

# Bootstrap the Secret Manager container without ever placing secret data in Terraform state.
Invoke-Native 'terraform' @(
    "-chdir=$infra", 'apply', '-input=false', '-auto-approve',
    '-target=google_secret_manager_secret.google_oauth_client_secret',
    "-var=project_id=$ProjectId",
    "-var=region=$Region",
    "-var=google_client_id=$GoogleClientId"
)
$secretId = 'wrapper-google-oauth-client-secret'
$enabledVersions = (Invoke-Capture 'gcloud' @(
    'secrets', 'versions', 'list', $secretId,
    "--project=$ProjectId",
    '--filter=state=ENABLED',
    '--format=value(name)'
) | Out-String).Trim()
if ([string]::IsNullOrWhiteSpace($enabledVersions)) {
    if ([string]::IsNullOrWhiteSpace($GoogleClientSecretFile)) {
        throw 'The Google OAuth secret has no enabled version. Pass GoogleClientSecretFile with the downloaded Desktop client JSON or a file containing only the client secret.'
    }
    $secretSource = [IO.Path]::GetFullPath($GoogleClientSecretFile)
    $secretText = [IO.File]::ReadAllText($secretSource).Trim()
    if ($secretText.StartsWith('{', [StringComparison]::Ordinal)) {
        $credential = $secretText | ConvertFrom-Json
        if ($null -ne $credential.installed) {
            $secretText = [string]$credential.installed.client_secret
        } elseif ($null -ne $credential.web) {
            $secretText = [string]$credential.web.client_secret
        } else {
            throw 'Google client JSON does not contain installed.client_secret.'
        }
    }
    if ([string]::IsNullOrWhiteSpace($secretText) -or $secretText -match '\s') {
        throw 'Google OAuth client secret is empty or malformed.'
    }
    $scratch = Join-Path $root '.task-tmp'
    New-Item -ItemType Directory -Path $scratch -Force | Out-Null
    $secretTemp = Join-Path $scratch ('.oauth-secret-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllText($secretTemp, $secretText, (New-Object Text.UTF8Encoding($false)))
        Invoke-Native 'gcloud' @(
            'secrets', 'versions', 'add', $secretId,
            "--project=$ProjectId",
            "--data-file=$secretTemp"
        )
    } finally {
        Remove-Variable secretText, credential -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $secretTemp -Force -ErrorAction SilentlyContinue
    }
}
if (-not [string]::IsNullOrWhiteSpace($BillingAccount)) {
    $budgetStateAddress = 'google_billing_budget.monthly[0]'
    $stateResources = @((Invoke-Capture 'terraform' @("-chdir=$infra", 'state', 'list')))
    if ($budgetStateAddress -notin $stateResources) {
        $existingBudgets = @(((Invoke-Capture 'gcloud' @(
            'billing', 'budgets', 'list',
            "--billing-account=$BillingAccount",
            '--format=json'
        ) | Out-String) | ConvertFrom-Json) | Where-Object { $_.displayName -eq 'Wrapper monthly budget' })
        if ($existingBudgets.Count -gt 1) {
            throw 'More than one existing Wrapper monthly budget was found; refusing an ambiguous import.'
        }
        if ($existingBudgets.Count -eq 1) {
            Invoke-Native 'terraform' @(
                "-chdir=$infra", 'import', '-input=false',
                "-var=project_id=$ProjectId",
                "-var=billing_account=$BillingAccount",
                "-var=monthly_budget_amount=$MonthlyBudgetAmount",
                "-var=google_client_id=$GoogleClientId",
                $budgetStateAddress, [string]$existingBudgets[0].name
            )
        }
    }
}
try {
    Invoke-Native 'terraform' @(
        "-chdir=$infra", 'plan', '-input=false', "-out=$planPath",
        "-var=project_id=$ProjectId",
        "-var=region=$Region",
        "-var=firestore_location=$FirestoreLocation",
        "-var=github_repository=$GitHubRepository",
        "-var=billing_account=$BillingAccount",
        "-var=monthly_budget_amount=$MonthlyBudgetAmount",
        "-var=google_client_id=$GoogleClientId"
    )
    Invoke-Native 'terraform' @("-chdir=$infra", 'apply', '-input=false', '-auto-approve', $planPath)
} finally {
    Remove-Item -LiteralPath $planPath -Force -ErrorAction SilentlyContinue
}

$apiUrl = (Invoke-Capture 'terraform' @("-chdir=$infra", 'output', '-raw', 'api_url') | Out-String).Trim()
$firebaseApiKey = (Invoke-Capture 'terraform' @("-chdir=$infra", 'output', '-raw', 'firebase_api_key') | Out-String).Trim()
$wifProvider = (Invoke-Capture 'terraform' @("-chdir=$infra", 'output', '-raw', 'workload_identity_provider') | Out-String).Trim()
$deployAccount = (Invoke-Capture 'terraform' @("-chdir=$infra", 'output', '-raw', 'github_deploy_service_account') | Out-String).Trim()

if ($ConfigureGitHub) {
    $variables = [ordered]@{
        GCP_PROJECT_ID                   = $ProjectId
        GCP_REGION                       = $Region
        GCP_WIF_PROVIDER                 = $wifProvider
        GCP_DEPLOY_SERVICE_ACCOUNT       = $deployAccount
        WRAPPER_CLOUD_API_URL            = $apiUrl
        WRAPPER_GOOGLE_CLIENT_ID         = $GoogleClientId
        WRAPPER_FIREBASE_API_KEY         = $firebaseApiKey
    }
    foreach ($entry in $variables.GetEnumerator()) {
        Invoke-Native 'gh' @(
            'variable', 'set', [string]$entry.Key,
            '--repo', $GitHubRepository,
            '--body', [string]$entry.Value
        )
    }
    Invoke-Native 'gh' @(
        'api', '--method', 'PUT',
        "repos/$GitHubRepository/environments/production"
    )
}

Remove-Variable firebaseApiKey -ErrorAction SilentlyContinue
Write-Host "Wrapper infrastructure is ready at $apiUrl"
if (-not $ConfigureGitHub) {
    Write-Host 'GitHub variables were not changed. Rerun with -ConfigureGitHub when ready.'
}
