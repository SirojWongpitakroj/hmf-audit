# Ingest the 100,000-log dataset that the demo scripts work on. This is a thin
# wrapper around ..\attribute-query\ingest.ps1 so the whole demo can be run from
# this folder.
#
#   .\ingest.ps1            ingest 100,000 logs (adds to what is already there)
#   .\ingest.ps1 -Reset     empty the demo's tables first, then ingest
#
# Local Docker only.
[CmdletBinding()]
param([switch]$Reset)
$ErrorActionPreference = "Stop"
$ingest = Join-Path $PSScriptRoot "..\attribute-query\ingest.ps1"
if ($Reset) { & $ingest -Records 100000 -Reset } else { & $ingest -Records 100000 }
exit $LASTEXITCODE
