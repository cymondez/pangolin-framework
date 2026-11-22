param([string]$Project = 'pangolin-m1-dev', [switch]$Race)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @('scripts/m1.py', 'verify', '--project', $Project)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw 'M1 cumulative gate failed; inspect artifacts report' }
} finally {
    Pop-Location
}
