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
$EverythingInstallerVersion = '1.4.1.1032'
$EverythingInstallerSha256 = '4e7a80885aab8566b750c56a2a2b3e7c3c1f4920bcc53777e07f5eeb9e1f7485'
$prerequisiteDirectory = Join-Path $root 'installer\prerequisites'
$everythingInstaller = Join-Path $prerequisiteDirectory "Everything-$EverythingInstallerVersion.x64.msi"
$everythingInstallerUrl = "https://www.voidtools.com/Everything-$EverythingInstallerVersion.x64.msi"
New-Item -ItemType Directory -Path $prerequisiteDirectory -Force | Out-Null

$installerReady = Test-Path -LiteralPath $everythingInstaller -PathType Leaf
if ($installerReady) {
    $installerReady = (Get-FileHash -LiteralPath $everythingInstaller -Algorithm SHA256).Hash.ToLowerInvariant() -eq
        $EverythingInstallerSha256.ToLowerInvariant()
}
if (-not $installerReady) {
    $download = "$everythingInstaller.download"
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $everythingInstallerUrl -OutFile $download
        $actualHash = (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actualHash -ne $EverythingInstallerSha256.ToLowerInvariant()) {
            throw "Everything installer checksum mismatch: $actualHash"
        }
        Move-Item -LiteralPath $download -Destination $everythingInstaller -Force
    } finally {
        Remove-Item -LiteralPath $download -Force -ErrorAction SilentlyContinue
    }
}

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
