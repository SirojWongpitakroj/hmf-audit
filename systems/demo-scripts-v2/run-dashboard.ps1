$ErrorActionPreference = 'Stop'
$node = Get-Command node -ErrorAction SilentlyContinue
if (-not $node) { throw 'Node.js is required to run the local dashboard.' }
$server = Join-Path $PSScriptRoot 'site/server.mjs'
Write-Host 'Starting the local log integrity dashboard at http://127.0.0.1:4170'
Write-Host 'Press Ctrl+C to stop.'
& $node.Source $server
