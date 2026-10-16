. (Join-Path $PSScriptRoot 'common.ps1')
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
# Do not copy real keys/passwords into release output. Preserve an existing deployment .env.
Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env.example' -Force
if (-not (Test-Path -LiteralPath 'build/bin/.env')) { Copy-Item -LiteralPath '.env.example' -Destination 'build/bin/.env' }
Write-Host 'Ready: build\bin\ScanSSH-ray.exe. Fill in build\bin\.env before use.'
