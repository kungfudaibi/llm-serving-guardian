$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Split-Path -Parent $PSScriptRoot)).Path
$binDirectory = (Resolve-Path (Join-Path $projectRoot 'bin')).Path
$pidFile = Join-Path $projectRoot '.run\pids.txt'

if (-not (Test-Path -LiteralPath $pidFile -PathType Leaf)) {
    Write-Host 'No demo PID file found.'
    exit 0
}

foreach ($processId in (Get-Content -LiteralPath $pidFile)) {
    $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
    if ($null -eq $process) { continue }
    $processPath = $process.Path
    if (-not $processPath.StartsWith($binDirectory, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to stop PID $processId outside project bin directory: $processPath"
    }
    Stop-Process -Id $processId
}
Remove-Item -LiteralPath $pidFile
Write-Host 'Demo processes stopped; log files remain under .run.'
