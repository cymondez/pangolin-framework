param([string]$Project = 'pangolin-m6-dev', [switch]$Race)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @('scripts/m6.py', 'verify', '--project', $Project)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw 'M6 cumulative release gate failed; inspect artifacts report' }
} finally {
    Pop-Location
}
