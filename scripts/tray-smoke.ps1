param([string]$Executable)

. (Join-Path $PSScriptRoot 'common.ps1')
if (-not $Executable) { $Executable = Join-Path $ProjectRoot 'build/bin/ScanSSH-ray.exe' }
$Executable = (Resolve-Path -LiteralPath $Executable).Path
$ExpectedVersion = (Get-Content -LiteralPath (Join-Path $ProjectRoot 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
if ((Get-Item -LiteralPath $Executable).VersionInfo.ProductVersion -ne $ExpectedVersion) {
    throw 'The executable version is stale. Run scripts/build.ps1 -TraySmoke.'
}
if (Get-Process -Name 'ScanSSH-ray' -ErrorAction SilentlyContinue) {
    throw 'Exit the running ScanSSH-ray through its tray menu before testing. The smoke test needs its own isolated instance.'
}
Write-Host "Testing existing EXE: $Executable (v$ExpectedVersion)"
node (Join-Path $PSScriptRoot 'tests/tray-smoke.mjs') $Executable
if ($LASTEXITCODE -ne 0) { throw 'Packaged tray smoke test failed.' }
