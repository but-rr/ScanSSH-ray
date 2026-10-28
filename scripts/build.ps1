param([switch]$TraySmoke)

. (Join-Path $PSScriptRoot 'common.ps1') -RequireWails
Push-Location -LiteralPath $ProjectRoot
try {
    & (Join-Path $PSScriptRoot 'test.ps1')
    $WindowsVersion = (Get-Content -LiteralPath 'wails.json' -Raw | ConvertFrom-Json).info.productVersion
    # The UI uses frontend/api.ts; generated Wails bindings are not needed.
    & $Wails build -s -skipbindings -trimpath -platform windows/amd64 -webview2 embed
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed.' }
    $ExecutableInfo = (Get-Item -LiteralPath 'build/bin/ScanSSH-ray.exe').VersionInfo
    if ($ExecutableInfo.FileVersion -ne $WindowsVersion -or $ExecutableInfo.ProductVersion -ne $WindowsVersion) {
        throw 'Windows executable version metadata does not match the application version.'
    }
    # Preserve the deployment configuration and never copy real keys/passwords.
    Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env.example' -Force
    if (-not (Test-Path -LiteralPath 'build/bin/.env')) { Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env' }
    if ($TraySmoke) { & (Join-Path $PSScriptRoot 'tray-smoke.ps1') }
    Write-Host "Ready: build\bin\ScanSSH-ray.exe (v$WindowsVersion). Fill in build\bin\.env before use."
} finally { Pop-Location }
