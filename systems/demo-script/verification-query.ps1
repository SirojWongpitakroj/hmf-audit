# Demo: audit a query by attribute against the ingested 100,000 logs.
#
#   .\verification-query.ps1 -Tenant T745 -Service spark-executor -LogType UNSPECIFIED -Region R1 -At 2015-09-01T18:14:40Z
#   .\verification-query.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 `
#              -Start 2017-03-01T00:00:00Z -End 2017-12-31T00:00:00Z
#
# Output: the log count, the log IDs, and Status VALID (verification passed) or
# REJECTED (verification failed). Exit code: 0 VALID, 1 REJECTED, 2 error.
# Options: -MaxIds N (IDs printed, default 100, -1 for all), -Json.
# Local Docker only. Ingest first with: systems\attribute-query\ingest.ps1 -Records 100000
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Tenant,
    [Parameter(Mandatory = $true)][string]$Service,
    [Parameter(Mandatory = $true)][string]$LogType,
    [Parameter(Mandatory = $true)][string]$Region,
    [string]$At = "",
    [string]$Start = "",
    [string]$End = "",
    [int]$MaxIds = 100,
    [switch]$Json
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")
$python = Assert-Demo100k
$audit = Invoke-AuditQuery -Tenant $Tenant -Service $Service -LogType $LogType -Region $Region -At $At -Start $Start -End $End
if ($audit.ExitCode -eq 2) {
    [Console]::Error.Write($audit.Stderr)
    exit 2
}
$outFile = [System.IO.Path]::GetTempFileName()
try {
    [System.IO.File]::WriteAllText($outFile, $audit.Stdout, (New-Object System.Text.UTF8Encoding($false)))
    $formatArguments = @((Join-Path $PSScriptRoot "format_result.py"), "--stdout", $outFile, "--query", $audit.Description,
        "--max-ids", "$MaxIds", "--records", "$script:ExpectedRecords")
    if ($Json) { $formatArguments += "--json" }
    & $python @formatArguments
    exit $LASTEXITCODE
} finally {
    Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
}
