# Query the ingested logs by attribute and audit them; every matching log is
# reported VALID or REJECTED.
#
#   .\query.ps1 -Tenant T745 -Service spark-executor -LogType UNSPECIFIED -Region R1 -At 2015-09-01T18:14:40Z
#   .\query.ps1 ... -Start 2015-09-01T18:00:00Z -End 2015-09-01T19:00:00Z -Limit 50
#   .\query.ps1 -Service spark-executor -LogType INFO -Region R0 -Start ... -End ...   (no -Tenant: all tenants, one query)
#
# Exit code: 0 all VALID, 1 at least one REJECTED, 2 error.
[CmdletBinding()]
param(
    [string]$Tenant = "",
    [Parameter(Mandatory = $true)][string]$Service,
    [Parameter(Mandatory = $true)][string]$LogType,
    [Parameter(Mandatory = $true)][string]$Region,
    [string]$At = "",
    [string]$Start = "",
    [string]$End = "",
    [int]$Limit = 20,
    [switch]$Json
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "lib.ps1")
$arguments = @("-key-file", $script:KeyFile, "-service", $Service,
    "-log-type", $LogType, "-region", $Region, "-limit", "$Limit")
if ($Tenant -ne "") { $arguments += @("-tenant", $Tenant) }
if ($At -ne "") { $arguments += @("-at", $At) }
if ($Start -ne "") { $arguments += @("-start", $Start) }
if ($End -ne "") { $arguments += @("-end", $End) }
if ($Json) { $arguments += "-json" }
$auditor = Build-Auditor
& $auditor @arguments
exit $LASTEXITCODE
