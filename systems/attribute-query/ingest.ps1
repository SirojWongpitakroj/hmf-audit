# Ingest a dataset through the real HMF-Audit write path so logs can be queried
# by attribute (tenant, service, log type, region, time) and audited.
# Exactly two sizes are accepted; anything else is rejected.
#
#   .\ingest.ps1 -Records 100000
#   .\ingest.ps1 -Records 10000000
#   .\ingest.ps1 -Records 100000 -Reset      empty this tool's tables first
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Records,
    [switch]$Reset
)
$ErrorActionPreference = "Stop"
# Exact match on the canonical decimal text: 1e5, 100,000, 0100000 and 10M are rejected.
if (@("100000", "10000000") -cnotcontains $Records) {
    Write-Error "rejected -Records '$Records': only 100000 (100k) and 10000000 (10 million) are allowed" -ErrorAction Continue
    exit 2
}
. (Join-Path $PSScriptRoot "lib.ps1")
foreach ($tool in @("go", "docker")) {
    if ($null -eq (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed" }
}
Set-Settings -Count 1 -Warmup 0 -BenchTime "1x" -PerRegion ([int]$Records / $script:Regions) -Dataset "" -OutputDirectory ""
Confirm-Dataset
Start-AttributeServices
Write-Note "ingesting $Records records"
$arguments = @("run", "./cmd/hmf-ingest", "-dataset", $script:DatasetPath, "-key-file", $script:KeyFile, "-keyspace", $script:Keyspace)
if ($Reset) { $arguments += "-reset" }
Invoke-Native -What "hmf-ingest" -Executable "go" -WorkingDirectory (Join-Path $script:SystemsDir "hmf-audit") -Arguments $arguments
