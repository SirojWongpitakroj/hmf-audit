# Find the tampered logs among the logs that satisfy an attribute query, and show
# each one's correct content next to its tampered content.
#
# The query is exactly the one verification-query.ps1 takes:
#   .\find-tampered.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 `
#                       -Start 2017-03-01T00:00:00Z -End 2017-12-31T00:00:00Z
#
# For every log that fails verification it prints:
#   Correct content   the log as ingested (from the source dataset)
#   Tampered content  the stored row, decrypted without authentication (needs the key)
#   Correct / tampered digest  the digest committed under the anchored root versus
#                     the digest of the row stored now (always shown; this is what
#                     verification compares, and it differs when the content changed)
# If a content cannot be seen, only the digests are shown for it.
#
# Exit code: 0 no tampered log, 1 tampered log(s) found, 2 error. Option: -Json.
# Local Docker only (the 100k ingest).
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Tenant,
    [Parameter(Mandatory = $true)][string]$Service,
    [Parameter(Mandatory = $true)][string]$LogType,
    [Parameter(Mandatory = $true)][string]$Region,
    [string]$At = "",
    [string]$Start = "",
    [string]$End = "",
    [switch]$Json
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")
$python = Assert-Demo100k
$audit = Invoke-AuditQuery -Tenant $Tenant -Service $Service -LogType $LogType -Region $Region -At $At -Start $Start -End $End -WithKey
if ($audit.ExitCode -eq 2) {
    [Console]::Error.Write($audit.Stderr)
    exit 2
}
$outFile = [System.IO.Path]::GetTempFileName()
try {
    [System.IO.File]::WriteAllText($outFile, $audit.Stdout, (New-Object System.Text.UTF8Encoding($false)))
    $arguments = @((Join-Path $PSScriptRoot "find_tampered.py"), "--stdout", $outFile, "--query", $audit.Description,
        "--dataset", $script:Source100k)
    if ($Json) { $arguments += "--json" }
    & $python @arguments
    exit $LASTEXITCODE
} finally {
    Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
}
