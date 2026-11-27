param(
    [string]$Project = 'pangolin-m6-dev',
    [switch]$Race,
    [ValidateSet('release', 'realtime')][string]$Scope = 'release'
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
if ($Project -notmatch '^pangolin-(m[67]|release|realtime)-[a-z0-9-]+$') {
    throw "project must be a dedicated pangolin-release-*, pangolin-realtime-*, pangolin-m6-*, or pangolin-m7-* project"
}
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @('scripts/verify.py', 'verify', '--project', $Project, '--scope', $Scope)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw "cumulative verification gate ($Scope) failed; inspect artifacts report" }
} finally {
    Pop-Location
}
