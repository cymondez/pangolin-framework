$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED
$previousGoTmp = $env:GOTMPDIR
$previousModCache = $env:GOMODCACHE
Push-Location -LiteralPath $repoRoot
try {
    New-Item -ItemType Directory -Force -Path artifacts,.cache/go-tmp,.cache/go-mod | Out-Null
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    $env:GOTMPDIR = Join-Path $repoRoot '.cache/go-tmp'
    $env:GOMODCACHE = Join-Path $repoRoot '.cache/go-mod'
    & go build -o artifacts/test-idp ./internal/testkit/cmd/idp
    if ($LASTEXITCODE -ne 0) { throw 'test IdP build failed' }
} finally {
    $env:GOOS = $previousGoOS
    $env:GOARCH = $previousGoArch
    $env:CGO_ENABLED = $previousCgo
    $env:GOTMPDIR = $previousGoTmp
    $env:GOMODCACHE = $previousModCache
    Pop-Location
}
