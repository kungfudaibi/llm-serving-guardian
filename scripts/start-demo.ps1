$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$runDirectory = Join-Path $projectRoot '.run'
$binDirectory = Join-Path $projectRoot 'bin'
$go = (Get-Command go -ErrorAction Stop).Source

New-Item -ItemType Directory -Force -Path $runDirectory, $binDirectory | Out-Null
$pidFile = Join-Path $runDirectory 'pids.txt'
if (Test-Path -LiteralPath $pidFile -PathType Leaf) {
    throw 'A demo PID file already exists. Run scripts\stop-demo.ps1 before starting another demo.'
}
& $go build -o (Join-Path $binDirectory 'guardian.exe') ./cmd/guardian
if ($LASTEXITCODE -ne 0) { throw 'guardian build failed' }
& $go build -o (Join-Path $binDirectory 'mock-worker.exe') ./cmd/mock-worker
if ($LASTEXITCODE -ne 0) { throw 'mock worker build failed' }

$processes = @(
    Start-Process -FilePath (Join-Path $binDirectory 'mock-worker.exe') -ArgumentList '-listen','127.0.0.1:18081','-name','mock-one' -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runDirectory 'mock-one.log') -RedirectStandardError (Join-Path $runDirectory 'mock-one.err.log')
    Start-Process -FilePath (Join-Path $binDirectory 'mock-worker.exe') -ArgumentList '-listen','127.0.0.1:18082','-name','mock-two' -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runDirectory 'mock-two.log') -RedirectStandardError (Join-Path $runDirectory 'mock-two.err.log')
    Start-Process -FilePath (Join-Path $binDirectory 'guardian.exe') -ArgumentList '-config','configs/demo.json' -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runDirectory 'guardian.log') -RedirectStandardError (Join-Path $runDirectory 'guardian.err.log')
)
$processes.Id | Set-Content -LiteralPath $pidFile

for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $ready = Invoke-RestMethod -Uri 'http://127.0.0.1:8090/readyz' -TimeoutSec 1
        if ($ready.status -eq 'ready') {
            Write-Host 'Demo ready: http://127.0.0.1:8090'
            Write-Host 'Run scripts\demo-failure.ps1 to demonstrate failover.'
            return
        }
    } catch { Start-Sleep -Milliseconds 250 }
}
& (Join-Path $PSScriptRoot 'stop-demo.ps1')
throw "demo did not become ready; inspect $runDirectory"
