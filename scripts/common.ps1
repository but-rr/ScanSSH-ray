param([switch]$RequireWails)

$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Install Go 1.26+ from https://go.dev/dl/ and reopen PowerShell.' }
if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw 'Install Node.js 22 LTS (22.12+) and reopen PowerShell.' }
if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'Install Node.js 22.12+ and reopen PowerShell.' }
$NodeVersion = [version](node -p 'process.versions.node')
if ($LASTEXITCODE -ne 0 -or $NodeVersion -lt [version]'22.12.0') { throw 'Node.js 22.12+ is required.' }

function Assert-ProjectVersions {
    $FrontendVersion = (Get-Content -LiteralPath (Join-Path $ProjectRoot 'frontend/package.json') -Raw | ConvertFrom-Json).version
    # Windows PowerShell 5.1 cannot parse package-lock's empty packages key.
    $LockVersions = node -p 'JSON.stringify([require(process.argv[1]).version, require(process.argv[1]).packages[''''].version])' (Join-Path $ProjectRoot 'frontend/package-lock.json')
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read frontend/package-lock.json.' }
    $LockVersions = $LockVersions | ConvertFrom-Json
    $WindowsVersion = (Get-Content -LiteralPath (Join-Path $ProjectRoot 'wails.json') -Raw | ConvertFrom-Json).info.productVersion
    if ($FrontendVersion -ne $WindowsVersion -or $FrontendVersion -ne $LockVersions[0] -or $FrontendVersion -ne $LockVersions[1]) {
        throw 'Versions in frontend/package.json, package-lock.json and wails.json must match.'
    }
}

function Install-FrontendDependencies {
    Push-Location -LiteralPath (Join-Path $ProjectRoot 'frontend')
    try {
        $LockHash = (Get-FileHash -LiteralPath 'package-lock.json' -Algorithm SHA256).Hash
        # npm ci deletes node_modules; an editor/dev server may lock esbuild.exe.
        # Reuse an existing install, but reject any unexpected lockfile changes.
        if (Test-Path -LiteralPath 'node_modules') {
            npm.cmd install --no-audit --no-fund --cache (Join-Path $ProjectRoot '.tools/npm-cache')
        } else {
            npm.cmd ci --no-audit --no-fund --cache (Join-Path $ProjectRoot '.tools/npm-cache')
        }
        if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed.' }
        if ((Get-FileHash -LiteralPath 'package-lock.json' -Algorithm SHA256).Hash -ne $LockHash) {
            throw 'npm changed package-lock.json. Review the dependency changes before building.'
        }
    } finally { Pop-Location }
}

Assert-ProjectVersions
if (-not $RequireWails) { return }
$ToolBin = Join-Path $ProjectRoot '.tools\bin'
New-Item -ItemType Directory -Force -Path $ToolBin | Out-Null
$Wails = Join-Path $ToolBin 'wails.exe'
$PreviousGoBin = $env:GOBIN
Push-Location -LiteralPath $ProjectRoot
try {
    $WailsVersion = (go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the Wails version.' }
    $InstalledWails = ''
    if (Test-Path -LiteralPath $Wails) {
        $InstalledWails = go version -m $Wails
        if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the installed Wails CLI.' }
    }
    if (($InstalledWails -join "`n") -notmatch ('(?m)^\s*mod\s+github.com/wailsapp/wails/v2\s+' + [regex]::Escape($WailsVersion) + '(?:\s|$)')) {
        $env:GOBIN = $ToolBin
        go install ('github.com/wailsapp/wails/v2/cmd/wails@' + $WailsVersion)
        if ($LASTEXITCODE -ne 0) { throw 'Wails installation failed.' }
    }
} finally {
    $env:GOBIN = $PreviousGoBin
    Pop-Location
}
