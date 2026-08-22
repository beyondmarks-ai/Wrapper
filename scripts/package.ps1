[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$EverythingDll,
    [string]$CloudApiUrl = $env:WRAPPER_CLOUD_API_URL,
    [string]$GoogleClientId = $env:WRAPPER_GOOGLE_CLIENT_ID,
    [string]$FirebaseApiKey = $env:WRAPPER_FIREBASE_API_KEY,
    [string]$CertificateThumbprint
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot 'build.ps1') -EverythingDll $EverythingDll -CloudApiUrl $CloudApiUrl -GoogleClientId $GoogleClientId -FirebaseApiKey $FirebaseApiKey -CertificateThumbprint $CertificateThumbprint

$iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
    $candidates = @(
        (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\Inno Setup 6\ISCC.exe'),
        (Join-Path ([Environment]::GetFolderPath('ProgramFilesX86')) 'Inno Setup 6\ISCC.exe'),
        (Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'Inno Setup 6\ISCC.exe')
    )
    $iscc = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}
if (-not $iscc) {
    throw 'Inno Setup 6 is required. Install it with: winget install JRSoftware.InnoSetup'
}

Push-Location $root
try {
    & $iscc (Join-Path $root 'installer\Wrapper.iss')
    if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed.' }
} finally {
    Pop-Location
}
