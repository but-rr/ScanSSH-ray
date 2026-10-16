. (Join-Path $PSScriptRoot 'common.ps1')
$env:SCANSSH_ENV = Join-Path $ProjectRoot '.env'
& $Wails dev -skipbindings
if ($LASTEXITCODE -ne 0) { throw 'Wails development session failed.' }
