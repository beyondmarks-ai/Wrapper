[CmdletBinding()]
param(
    [switch]$SkipTests,
    [string]$OutputDir,
    [string]$EverythingDll,
    [string]$CloudApiUrl = $env:WRAPPER_CLOUD_API_URL,
    [string]$GoogleClientId = $env:WRAPPER_GOOGLE_CLIENT_ID,
    [string]$FirebaseApiKey = $env:WRAPPER_FIREBASE_API_KEY,
    [string]$CertificateThumbprint
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $projectRoot 'bin'
}
$outputDirPath = [IO.Path]::GetFullPath($OutputDir)
$rootPath = [IO.Path]::GetFullPath($projectRoot)
if (-not $outputDirPath.StartsWith($rootPath, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'OutputDir must remain inside the Wrapper repository.'
}

function Invoke-GoBuild {
    param([string]$Package, [string]$Output, [string]$LinkerFlags)
    $arguments = @('build', '-trimpath', '-buildvcs=true')
    if ($LinkerFlags) {
        $arguments += @('-ldflags', $LinkerFlags)
    }
    $arguments += @('-o', $Output, $Package)
    & go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed for $Package."
    }
}

Push-Location $projectRoot
try {
    New-Item -ItemType Directory -Path $outputDirPath -Force | Out-Null

    if (-not $SkipTests) {
        Write-Host 'Running Go tests...'
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
    }

    $module = 'github.com/beyondmarks-ai/Wrapper/src/config'
    $values = @{
        DefaultCloudAPIURL        = $CloudApiUrl
        DefaultGoogleClientID     = $GoogleClientId
        DefaultFirebaseAPIKey     = $FirebaseApiKey
    }
    $linkerParts = @('-s', '-w')
    foreach ($entry in $values.GetEnumerator()) {
        if (-not [string]::IsNullOrWhiteSpace([string]$entry.Value)) {
            if ([string]$entry.Value -match '\s') {
                throw "$($entry.Key) cannot contain whitespace when embedded at build time."
            }
            $linkerParts += "-X=$module.$($entry.Key)=$($entry.Value)"
        }
    }
    $linkerFlags = $linkerParts -join ' '

    Write-Host 'Building wrap.exe and wrapper-agent.exe...'
    Invoke-GoBuild -Package '.' -Output (Join-Path $outputDirPath 'wrap.exe') -LinkerFlags $linkerFlags
    Invoke-GoBuild -Package './cmd/wrapper-agent' -Output (Join-Path $outputDirPath 'wrapper-agent.exe') -LinkerFlags $linkerFlags

    if ($EverythingDll) {
        $dllPath = (Resolve-Path -LiteralPath $EverythingDll).Path
        if ([IO.Path]::GetExtension($dllPath) -ne '.dll') {
            throw 'EverythingDll must point to an Everything SDK DLL.'
        }
        $dllDestination = Join-Path $outputDirPath 'Everything64.dll'
        if (-not [string]::Equals($dllPath, [IO.Path]::GetFullPath($dllDestination), [StringComparison]::OrdinalIgnoreCase)) {
            Copy-Item -LiteralPath $dllPath -Destination $dllDestination -Force
        }
    }

    if ($CertificateThumbprint) {
        $signtool = (Get-Command signtool.exe -ErrorAction Stop).Source
        foreach ($binary in @('wrap.exe', 'wrapper-agent.exe')) {
            & $signtool sign /sha1 $CertificateThumbprint /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 (Join-Path $outputDirPath $binary)
            if ($LASTEXITCODE -ne 0) { throw "Code signing failed for $binary." }
        }
    }

    Get-FileHash -Algorithm SHA256 (Join-Path $outputDirPath '*.exe') |
        Select-Object Hash, @{Name='File';Expression={ Split-Path -Leaf $_.Path }} |
        Format-Table -HideTableHeaders |
        Out-File -Encoding ascii (Join-Path $outputDirPath 'SHA256SUMS.txt')

    Write-Host "Build ready: $outputDirPath"
} finally {
    Pop-Location
}
