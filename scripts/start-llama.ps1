param(
    [string]$Model = 'ggml-org/gemma-3-1b-it-GGUF:Q4_K_M',
    [int]$Port = 8081,
    [int]$ContextSize = 2048
)

$ErrorActionPreference = 'Stop'
$llamaServer = 'D:\Downloads\llama-b10343-bin-win-cuda-12.4-x64\llama-server.exe'
$cacheDirectory = 'F:\llm-serving-guardian\llama-cache'

if (-not (Test-Path -LiteralPath $llamaServer -PathType Leaf)) {
    throw "llama-server.exe not found at $llamaServer"
}
New-Item -ItemType Directory -Force -Path $cacheDirectory | Out-Null
$env:LLAMA_CACHE = $cacheDirectory

Write-Host "LLAMA_CACHE=$cacheDirectory"
Write-Host "Starting llama.cpp on http://127.0.0.1:$Port; the first run downloads the model to F:."
& $llamaServer -hf $Model --host 127.0.0.1 --port $Port --ctx-size $ContextSize --n-gpu-layers 99
