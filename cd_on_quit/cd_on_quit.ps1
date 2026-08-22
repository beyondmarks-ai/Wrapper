function wrap() {
    param (
        [string[]]$Params
    )
    $wrap_location = [Environment]::GetFolderPath("LocalApplicationData") + "\Programs\wrapper\wrap.exe"
    $WRAP_LAST_DIR_PATH = [Environment]::GetFolderPath("LocalApplicationData") + "\wrapper\lastdir"

    & $wrap_location @Params

    if (Test-Path $WRAP_LAST_DIR_PATH) {
        $WRAP_LAST_DIR = Get-Content -Path $WRAP_LAST_DIR_PATH
        Invoke-Expression $WRAP_LAST_DIR
        Remove-Item -Force $WRAP_LAST_DIR_PATH
    }
}
