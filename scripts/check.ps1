param([switch]$Race)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
function Invoke-Go {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$GoArguments)
    & go @GoArguments
    if ($LASTEXITCODE -ne 0) { throw "go $($GoArguments -join ' ') failed ($LASTEXITCODE)" }
}
Push-Location -LiteralPath $repoRoot
$previousGoTmp = $env:GOTMPDIR
$previousModCache = $env:GOMODCACHE
try {
    New-Item -ItemType Directory -Force -Path artifacts | Out-Null
    New-Item -ItemType Directory -Force -Path .cache/go-tmp | Out-Null
    New-Item -ItemType Directory -Force -Path .cache/go-mod | Out-Null
    $env:GOTMPDIR = Join-Path $repoRoot '.cache/go-tmp'
    $env:GOMODCACHE = Join-Path $repoRoot '.cache/go-mod'
    Invoke-Go 'build' './...'
    Invoke-Go 'vet' './...'
    Invoke-Go 'run' './tools/check'
    $coverPackages = ((& go list ./...) | Where-Object { $_ -notlike '*/internal/testkit/*' }) -join ','
    if ($LASTEXITCODE -ne 0) { throw 'go list failed' }
    Invoke-Go 'test' "-coverpkg=$coverPackages" '-covermode=atomic' '-coverprofile=artifacts/unit.out' './...'
    Invoke-Go 'run' './tools/check' '-coverage' 'artifacts/unit.out'
    & python scripts/m0.py check
    if ($LASTEXITCODE -ne 0) { throw 'M0 baseline checks failed' }
    & python -m unittest discover -s tests/m0 -p 'test_*.py'
    if ($LASTEXITCODE -ne 0) { throw 'M0 regression tests failed' }
    if ($Race) { Invoke-Go 'test' '-race' './...' }
} finally {
    $env:GOTMPDIR = $previousGoTmp
    $env:GOMODCACHE = $previousModCache
    Pop-Location
}
