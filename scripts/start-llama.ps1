param(
    [string]$ModelPath = 'F:\llm-serving-guardian\models\gemma-3-1b-it-Q4_K_M.gguf',
    [int]$Port = 8081,
    [int]$ContextSize = 2048
)

$ErrorActionPreference = 'Stop'
$llamaServer = 'D:\Downloads\llama-b10343-bin-win-cuda-12.4-x64\llama-server.exe'
$cacheDirectory = 'F:\llm-serving-guardian\llama-cache'
$downloadUrl = 'https://huggingface.co/ggml-org/gemma-3-1b-it-GGUF/resolve/main/gemma-3-1b-it-Q4_K_M.gguf?download=true'
$expectedLength = 806058240
$expectedSha256 = '8CCC5CD1F1B3602548715AE25A66ED73FD5DC68A210412EEA643EB20EB75A135'

if (-not (Test-Path -LiteralPath $llamaServer -PathType Leaf)) {
    throw "llama-server.exe not found at $llamaServer"
}
New-Item -ItemType Directory -Force -Path $cacheDirectory, (Split-Path -Parent $ModelPath) | Out-Null
$env:LLAMA_CACHE = $cacheDirectory

if (-not (Test-Path -LiteralPath $ModelPath -PathType Leaf)) {
    $partPath = "$ModelPath.part"
    Write-Host "Downloading the 806 MB Q4 model to $ModelPath"
    Invoke-WebRequest -Uri $downloadUrl -OutFile $partPath -MaximumRedirection 10 -TimeoutSec 1800
    $actualLength = (Get-Item -LiteralPath $partPath).Length
    if ($actualLength -ne $expectedLength) {
        throw "Downloaded length $actualLength does not match expected length $expectedLength"
    }
    $actualSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $partPath).Hash
    if ($actualSha256 -ne $expectedSha256) {
        throw "Downloaded SHA-256 $actualSha256 does not match the official model digest"
    }
    Move-Item -LiteralPath $partPath -Destination $ModelPath -Force
}

Write-Host "LLAMA_CACHE=$cacheDirectory"
Write-Host "Starting llama.cpp on http://127.0.0.1:$Port with $ModelPath"
& $llamaServer -m $ModelPath --host 127.0.0.1 --port $Port --ctx-size $ContextSize --n-gpu-layers 99
