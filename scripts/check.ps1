param([string]$Project = 'pangolin-m6-dev', [switch]$Race, [int]$Milestone = 6)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$script = Join-Path $repoRoot ("scripts/m{0}.py" -f $Milestone)
if (-not (Test-Path $script)) { throw "unknown milestone gate: $Milestone" }
if ($Project -notlike ("pangolin-m{0}-*" -f $Milestone)) { throw "project must be a dedicated pangolin-m$Milestone-* project" }
Push-Location -LiteralPath $repoRoot
try {
    $arguments = @("scripts/m$Milestone.py", 'verify', '--project', $Project)
    if ($Race) { $arguments += '--race' }
    & python @arguments
    if ($LASTEXITCODE -ne 0) { throw "M$Milestone cumulative gate failed; inspect artifacts report" }
} finally {
    Pop-Location
}
