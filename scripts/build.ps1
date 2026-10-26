. (Join-Path $PSScriptRoot 'common.ps1')
$FrontendVersion = (Get-Content -LiteralPath 'frontend/package.json' -Raw | ConvertFrom-Json).version
$WindowsVersion = (Get-Content -LiteralPath 'wails.json' -Raw | ConvertFrom-Json).info.productVersion
if ($FrontendVersion -ne $WindowsVersion) { throw 'Versions in frontend/package.json and wails.json must match.' }
Push-Location -LiteralPath (Join-Path $ProjectRoot 'frontend')
try {
    # Reuse installed native modules: Windows may lock them while an editor is open.
    npm.cmd install --no-audit --no-fund --cache (Join-Path $ProjectRoot '.tools/npm-cache')
    if ($LASTEXITCODE -ne 0) { throw 'npm install failed.' }
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend checks failed.' }
} finally { Pop-Location }
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
# The UI uses frontend/api.ts; generated Wails bindings are not needed.
& $Wails build -s -skipbindings -trimpath -platform windows/amd64 -webview2 embed
if ($LASTEXITCODE -ne 0) { throw 'Windows build failed.' }
$ExecutableInfo = (Get-Item -LiteralPath 'build/bin/ScanSSH-ray.exe').VersionInfo
if ($ExecutableInfo.FileVersion -ne $WindowsVersion -or $ExecutableInfo.ProductVersion -ne $WindowsVersion) {
    throw 'Windows executable version metadata does not match the application version.'
}
# Do not copy real keys/passwords into release output. Preserve an existing deployment .env.
Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env.example' -Force
if (-not (Test-Path -LiteralPath 'build/bin/.env')) { Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env' }
Write-Host 'Ready: build\bin\ScanSSH-ray.exe. Fill in build\bin\.env before use.'
