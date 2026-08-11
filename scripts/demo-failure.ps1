$ErrorActionPreference = 'Stop'
$body = @{ model = 'guardian-mock'; messages = @(@{ role = 'user'; content = 'hello' }) } | ConvertTo-Json -Depth 4

Write-Host '1. Healthy workers:'
Invoke-RestMethod -Uri 'http://127.0.0.1:8090/admin/workers' | Format-Table name,isHealthy,circuitState,consecutiveFailures

Write-Host '2. Enable failures on mock-one and send two requests.'
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18081/admin/failure?enabled=true' | Out-Null
1..2 | ForEach-Object {
    $response = Invoke-WebRequest -Method Post -Uri 'http://127.0.0.1:8090/v1/chat/completions' -ContentType 'application/json' -Body $body
    Write-Host "request $_ -> worker=$($response.Headers['X-Guardian-Worker']) attempts=$($response.Headers['X-Guardian-Attempts']) status=$($response.StatusCode)"
}
Start-Sleep -Seconds 3
Invoke-RestMethod -Uri 'http://127.0.0.1:8090/admin/workers' | Format-Table name,isHealthy,circuitState,consecutiveFailures

Write-Host '3. Recover mock-one; health probes will return it to rotation.'
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:18081/admin/failure?enabled=false' | Out-Null
for ($attempt = 0; $attempt -lt 20; $attempt++) {
    Start-Sleep -Seconds 1
    $workers = Invoke-RestMethod -Uri 'http://127.0.0.1:8090/admin/workers'
    $mockOne = $workers | Where-Object { $_.name -eq 'mock-one' }
    if ($mockOne.isHealthy) { break }
}
Invoke-RestMethod -Uri 'http://127.0.0.1:8090/admin/workers' | Format-Table name,isHealthy,circuitState,consecutiveFailures
