# Tamper with ONE existing log that satisfies an attribute query: the log's
# stored ciphertext is modified in place (no log is inserted or removed) in the
# storage provider's table, hmfq.encrypted_logs. The committed digest in the
# hierarchy is untouched, so verification of the same query now REJECTS it.
#
# The query is exactly the one verification-query.ps1 takes:
#   .\tamper.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 `
#                -Start 2017-03-01T00:00:00Z -End 2017-12-31T00:00:00Z
#   .\tamper.ps1 <query> -Index 3     modify the 4th matching log (default: the first)
#   .\tamper.ps1 <query> -Verify      then run verification-query.ps1 with the same query
#   .\tamper.ps1 -Restore             undo the recorded tampering
#
# Exit code: 0 done, 2 error. Local Docker only (the 100k ingest).
[CmdletBinding()]
param(
    [string]$Tenant = "", [string]$Service = "", [string]$LogType = "", [string]$Region = "",
    [string]$At = "", [string]$Start = "", [string]$End = "",
    [int]$Index = 0, [switch]$Verify, [switch]$Restore
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")
$state = Join-Path $PSScriptRoot ".tamper-state"

function Read-State {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $state) {
        $key, $value = $line -split "=", 2
        $values[$key] = $value
    }
    return $values
}
function Get-RowHash([string]$id) {
    return ((Invoke-Provider "SELECT encode(sha256(ciphertext),'hex') FROM hmfq.encrypted_logs WHERE log_id = '$id'") -join "")
}
function Switch-Byte([string]$id) {
    return @(Invoke-Provider ("UPDATE hmfq.encrypted_logs SET ciphertext = set_byte(ciphertext, 0, get_byte(ciphertext, 0) # 255) " +
        "WHERE log_id = '$id' RETURNING encode(sha256(ciphertext),'hex')"))
}

if ($Restore) {
    if (-not (Test-Path -LiteralPath $state)) { Stop-Demo "nothing to restore: no tampering is recorded" }
    $recorded = Read-State
    $id = $recorded["log_id"]
    $current = Get-RowHash $id
    if ($current -eq $recorded["original_sha256"]) {
        Write-Note "log $id already holds its original content"
    } elseif ($current -eq $recorded["tampered_sha256"] -or $recorded["tampered_sha256"] -eq "") {
        # An empty tampered hash means the tampering was interrupted before it was
        # recorded. The byte flip is its own inverse, so apply it and check the result.
        $restored = @(Switch-Byte $id)
        if ($restored.Count -ne 1 -or $restored[0] -ne $recorded["original_sha256"]) {
            [void](Switch-Byte $id)
            Stop-Demo "restore did not reproduce the original content for $id (row left as it was found); stop and inspect"
        }
        Write-Note "restored log $id to its original content"
    } else {
        Stop-Demo "log $id was changed by something else since the tampering; not touching it"
    }
    Remove-Item -LiteralPath $state -Force
    exit 0
}

if ($Tenant -eq "" -or $Service -eq "" -or $LogType -eq "" -or $Region -eq "") {
    Stop-Demo "give the attribute query: -Tenant, -Service, -LogType, -Region and a time (see the header of this script)"
}
if ($Index -lt 0) { Stop-Demo "-Index must be a non-negative integer" }
if (Test-Path -LiteralPath $state) {
    $recordedId = (Read-State)["log_id"]
    Stop-Demo "a tampered log is already recorded ($recordedId); run: .\tamper.ps1 -Restore"
}
$python = Assert-Demo100k
$audit = Invoke-AuditQuery -Tenant $Tenant -Service $Service -LogType $LogType -Region $Region -At $At -Start $Start -End $End
if ($audit.ExitCode -eq 2) {
    [Console]::Error.Write($audit.Stderr)
    exit 2
}
$outFile = [System.IO.Path]::GetTempFileName()
try {
    [System.IO.File]::WriteAllText($outFile, $audit.Stdout, (New-Object System.Text.UTF8Encoding($false)))
    $pick = Invoke-Capture -Executable $python -Arguments @((Join-Path $PSScriptRoot "pick_target.py"), "--stdout", $outFile, "--index", "$Index")
} finally {
    Remove-Item -LiteralPath $outFile -Force -ErrorAction SilentlyContinue
}
if ($pick.ExitCode -ne 0) {
    [Console]::Error.Write($pick.Stderr)
    exit 2
}
$count, $id = $pick.Stdout.Trim() -split " ", 2
if ($id -notmatch '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') { Stop-Demo "unexpected log ID '$id'" }

$original = Get-RowHash $id
if ($original -eq "") { Stop-Demo "log $id is not in the provider's table" }
Set-Content -LiteralPath $state -Encoding ascii -Value @("log_id=$id", "original_sha256=$original", "tampered_sha256=")
try {
    $changed = @(Switch-Byte $id)
} catch {
    Remove-Item -LiteralPath $state -Force
    throw
}
if ($changed.Count -ne 1 -or $changed[0] -eq $original) {
    Remove-Item -LiteralPath $state -Force
    Stop-Demo "the modification did not change exactly one row; nothing recorded"
}
$tampered = $changed[0]
Set-Content -LiteralPath $state -Encoding ascii -Value @("log_id=$id", "original_sha256=$original", "tampered_sha256=$tampered")

Write-Host "Query:    $($audit.Description)"
Write-Host "Matches:  $count log(s); modified #$Index"
Write-Host "Tampered: $id"
Write-Host "          one byte of its stored ciphertext was changed in place (no log inserted or removed)"
Write-Host "          content hash  $original"
Write-Host "                    ->  $tampered"
Write-Host "Undo:     .\tamper.ps1 -Restore"
if ($Verify) {
    Write-Host ""
    & (Join-Path $PSScriptRoot "verification-query.ps1") -Tenant $Tenant -Service $Service -LogType $LogType -Region $Region `
        -At $At -Start $Start -End $End
}
exit 0
