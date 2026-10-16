$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $ProjectRoot
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Install Go 1.26+ from https://go.dev/dl/ and reopen PowerShell.' }
if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw 'Install Node.js 22 LTS (22.12+) and reopen PowerShell.' }
$ToolBin = Join-Path $ProjectRoot '.tools\bin'
New-Item -ItemType Directory -Force -Path $ToolBin | Out-Null
$Wails = Join-Path $ToolBin 'wails.exe'
if (-not (Test-Path -LiteralPath $Wails)) {
    $WailsVersion = (go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the Wails version.' }
    $env:GOBIN = $ToolBin
    go install ('github.com/wailsapp/wails/v2/cmd/wails@' + $WailsVersion)
    if ($LASTEXITCODE -ne 0) { throw 'Wails installation failed.' }
}
if (-not (Test-Path -LiteralPath '.env')) { Copy-Item -LiteralPath '.env.example' -Destination '.env' }
