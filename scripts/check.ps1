param([string]$Project = 'pangolin-m3-dev', [switch]$Race)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @('scripts/m3.py', 'verify', '--project', $Project)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw 'M3 cumulative gate failed; inspect artifacts report' }
} finally {
    Pop-Location
}
