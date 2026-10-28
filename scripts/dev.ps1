. (Join-Path $PSScriptRoot 'common.ps1') -RequireWails
$PreviousScanSSHEnv = $env:SCANSSH_ENV
Push-Location -LiteralPath $ProjectRoot
try {
    Install-FrontendDependencies
    if (-not $env:SCANSSH_ENV) {
        if (-not (Test-Path -LiteralPath '.env')) { Copy-Item -LiteralPath '.env.example' -Destination '.env' }
        $env:SCANSSH_ENV = Join-Path $ProjectRoot '.env'
    }
    & $Wails dev -skipbindings
    if ($LASTEXITCODE -ne 0) { throw 'Wails development session failed.' }
} finally {
    $env:SCANSSH_ENV = $PreviousScanSSHEnv
    Pop-Location
}
