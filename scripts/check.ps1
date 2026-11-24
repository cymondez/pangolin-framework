param([string]$Project = 'pangolin-m5-dev', [switch]$Race)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @('scripts/m5.py', 'verify', '--project', $Project)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw 'M5 cumulative gate failed; inspect artifacts report' }
} finally {
    Pop-Location
}
