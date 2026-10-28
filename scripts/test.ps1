. (Join-Path $PSScriptRoot 'common.ps1')
Push-Location -LiteralPath $ProjectRoot
try {
    Install-FrontendDependencies
    Push-Location -LiteralPath (Join-Path $ProjectRoot 'frontend')
    try {
        npm.cmd test
        if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed.' }
        npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend checks failed.' }
    } finally { Pop-Location }
    go test -count=1 -timeout=2m ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed.' }
} finally { Pop-Location }
