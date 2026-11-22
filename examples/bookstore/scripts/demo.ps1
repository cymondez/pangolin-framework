param([ValidateSet('up','down','status')][string]$Action = 'up')
$ErrorActionPreference = 'Stop'
$demoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$composeFile = Join-Path $demoRoot 'deploy/compose/m1.yaml'
$project = 'pangolin-bookstore-demo'
$composeArgs = @('compose','-p',$project,'-f',$composeFile)
function Invoke-DockerChecked([string[]]$DockerArguments) {
    $result = & docker @DockerArguments
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed: $($DockerArguments[0])" }
    return $result
}
# Read-only inventory precedes every service mutation. Never restart the Engine.
$null = Invoke-DockerChecked @('info','--format','{{.ServerVersion}}')
$inventory = @(Invoke-DockerChecked @('ps','-a','--format','{{json .}}') | ForEach-Object { $_ | ConvertFrom-Json })
$null = Invoke-DockerChecked @('compose','ls','--all','--format','json')
$null = Invoke-DockerChecked @('volume','ls','--format','{{.Name}}')
$null = Invoke-DockerChecked @('network','ls','--format','{{.Name}}')
$null = Invoke-DockerChecked @('stats','--no-stream','--format','{{.Name}} {{.MemUsage}}')
$ownedIds = @(Invoke-DockerChecked @('ps','-aq','--filter',"label=com.docker.compose.project=$project"))
foreach ($containerId in $ownedIds) {
    $details = (Invoke-DockerChecked @('inspect',$containerId) | ConvertFrom-Json)[0]
    $labels = $details.Config.Labels
    if ($labels.'dev.pangolin.owner' -ne 'pangolin-framework' -or
        $labels.'com.docker.compose.project.config_files' -ne $composeFile) {
        throw "Project $project belongs to another environment; choose a different demo setup."
    }
}
Write-Host "Demo project: $project. Other containers will remain untouched."
if ($Action -eq 'status') { Invoke-DockerChecked ($composeArgs + @('ps')); exit }
if ($Action -eq 'down') { Invoke-DockerChecked ($composeArgs + @('down')); exit }
$env:PANGOLIN_M1_PG_PORT = '15542'
$env:PANGOLIN_M1_NATS_PORT = '14322'
$env:PANGOLIN_M1_IDP_PORT = '18180'
if ($ownedIds.Count -eq 0 -or -not ($inventory | Where-Object { $_.Names -like "$project-*" -and $_.State -eq 'running' })) {
    foreach ($port in @(15542,14322,18180)) {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,$port)
        try { $listener.Start() } catch { throw "Loopback port $port is busy. Stop or reconfigure your own demo before continuing." }
        finally { $listener.Stop() }
    }
}
Invoke-DockerChecked ($composeArgs + @('config','--quiet'))
Push-Location $demoRoot
try {
    & (Join-Path $demoRoot 'scripts/build-test-idp.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'Could not build the demo login service.' }
    Invoke-DockerChecked ($composeArgs + @('up','-d','--build','--wait','--wait-timeout','60'))
    $psql = $composeArgs + @('exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','pangolin_bootstrap','-d','pangolin_m0')
    $exists = (Invoke-DockerChecked ($psql + @('-tAc',"SELECT to_regclass('pangolin.books') IS NOT NULL")) | Out-String).Trim()
    if ($exists -ne 't') {
        $schema = Get-Content -LiteralPath (Join-Path $demoRoot 'examples/bookstore/infrastructure/sql/schema.sql') -Raw
        "BEGIN; SET LOCAL ROLE pangolin_owner;`n$schema`nCOMMIT;" | & docker @psql
        if ($LASTEXITCODE -ne 0) { throw 'Could not initialize the demo database.' }
    }
    $env:PANGOLIN_NATS_URL = 'nats://127.0.0.1:14322'
    Push-Location (Join-Path $demoRoot 'examples/basics')
    try {
        & go run ./jetstream -provision
        if ($LASTEXITCODE -ne 0) { throw 'Could not initialize the demo event stream.' }
    } finally { Pop-Location }
    Write-Host 'Bookstore services are ready. Start the API and Worker using the tutorial.'
} finally { Pop-Location }
