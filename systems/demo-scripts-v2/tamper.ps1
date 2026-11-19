[CmdletBinding()]
param(
    [string]$Tenant,
    [string]$Service,
    [string]$LogType,
    [string]$Region,
    [string]$At,
    [string]$Start,
    [string]$End,
    [ValidateRange(1, 256)][int]$GoCpus = 4,
    [switch]$Restore
)

. (Join-Path $PSScriptRoot 'common.ps1')
$statePath = Join-Path $PSScriptRoot '.tamper-state.json'

function Invoke-DemoSql {
    param([string]$Sql)
    # Explicit local endpoint keeps the update on this machine's demo database.
    $endpoint = if ($env:OS -eq 'Windows_NT') { 'npipe:////./pipe/docker_engine' } else { 'unix:///var/run/docker.sock' }
    $reply = Invoke-Engine docker @('--host', $endpoint, 'exec', 'fair-postgres', 'psql',
        '-X', '-q', '-t', '-A', '-U', 'fair', '-d', 'fair_benchmark', '-v', 'ON_ERROR_STOP=1', '-c', $Sql)
    Assert-Exit $reply
    return $reply.Lines
}

function Read-CiphertextHashes {
    param([string]$LogId, [string]$Lid)
    if ($LogId -cnotmatch '^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$' -or $Lid -cnotmatch '^\d{12}$') {
        throw 'Invalid demo log identity.'
    }
    $sql = @"
SELECT encode(sha256(h.ciphertext), 'hex'),
       encode(sha256(set_byte(h.ciphertext, 0, get_byte(h.ciphertext, 0) # 255)), 'hex'),
       encode(sha256(e.ct_log), 'hex'),
       encode(sha256(set_byte(e.ct_log, 0, get_byte(e.ct_log, 0) # 255)), 'hex')
FROM hmfq.encrypted_logs h CROSS JOIN evseb.encrypted_logs e
WHERE h.log_id = '$LogId' AND e.lid = '$Lid'
  AND octet_length(h.ciphertext) > 0 AND octet_length(e.ct_log) > 0;
"@
    $hashes = ((Invoke-DemoSql $sql) -join '') -split '\|'
    if ($hashes.Count -ne 4 -or @($hashes | Where-Object { $_ -cnotmatch '^[a-f0-9]{64}$' }).Count -ne 0) {
        throw 'The target must have a nonempty ciphertext in each demo table.'
    }
    return $hashes
}

function Switch-DemoCiphertexts {
    param([string]$LogId, [string]$Lid, [string[]]$ExpectedHashes)
    # Identities and hashes are constrained before interpolation. Both writes commit together.
    if ($LogId -cnotmatch '^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$' -or $Lid -cnotmatch '^\d{12}$' -or
        $ExpectedHashes.Count -ne 2 -or @($ExpectedHashes | Where-Object { $_ -cnotmatch '^[a-f0-9]{64}$' }).Count -ne 0) {
        throw 'Invalid tamper transition.'
    }
    $sql = @"
DO `$demo`$
BEGIN
    UPDATE hmfq.encrypted_logs
       SET ciphertext = set_byte(ciphertext, 0, get_byte(ciphertext, 0) # 255)
     WHERE log_id = '$LogId' AND encode(sha256(ciphertext), 'hex') = '$($ExpectedHashes[0])';
    IF NOT FOUND THEN RAISE EXCEPTION 'HMF-Audit ciphertext changed before update'; END IF;
    UPDATE evseb.encrypted_logs
       SET ct_log = set_byte(ct_log, 0, get_byte(ct_log, 0) # 255)
     WHERE lid = '$Lid' AND encode(sha256(ct_log), 'hex') = '$($ExpectedHashes[1])';
    IF NOT FOUND THEN RAISE EXCEPTION 'EVSEB ciphertext changed before update'; END IF;
END;
`$demo`$;
"@
    $null = Invoke-DemoSql $sql
}

$temp = $null
$saved = $null
try {
    if ($Restore) {
        if (-not (Test-Path -LiteralPath $statePath)) { throw 'There is no saved demo tamper operation to restore.' }
        $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
        $current = Read-CiphertextHashes $state.LogId $state.EvsebLid
        if ($current[0] -ceq $state.HmfAfter -and $current[2] -ceq $state.EvsebAfter -and
            $current[1] -ceq $state.HmfBefore -and $current[3] -ceq $state.EvsebBefore) {
            Switch-DemoCiphertexts $state.LogId $state.EvsebLid @($state.HmfAfter, $state.EvsebAfter)
        } elseif ($current[0] -cne $state.HmfBefore -or $current[2] -cne $state.EvsebBefore) {
            throw 'The ciphertext hashes differ from the recorded demo state. Restore stopped before writing.'
        }
        Remove-Item -LiteralPath $statePath
        Write-Host "Original ciphertext confirmed for $($state.LogId) in both schemes."
        Publish-DemoEvent 'restored' @{ id = $state.LogId; message = 'Original ciphertext restored in HMF-Audit and EVSEB' }
        exit 0
    }
    if (Test-Path -LiteralPath $statePath) {
        throw 'A demo tamper operation is already recorded. Run tamper.ps1 -Restore first.'
    }
    $request = Get-DemoQuery $PSBoundParameters
    $temp = New-DemoWorkspace
    $saved = Save-GoEnvironment $GoCpus
    Write-Host "Query $($request.Description)"
    Start-DemoScheme 'HMF-Audit' 'Authenticating the batch in HMF-Audit'
    $audit = Invoke-HMFAudit (Join-Path $temp 'hmf-auditor.exe') $request.Flags 0
    if (@($audit.Rows).Count -eq 0 -or -not $audit.LocatorValid -or $audit.Code -ne 0) {
        throw 'Tampering requires a nonempty, valid HMF-Audit batch.'
    }
    $logIds = @($audit.Rows | ForEach-Object { $_.log_id })
    $target = $logIds[0]
    Write-Host "First authenticated log ID $target"
    $identities = Read-SourceIdentities $logIds
    $lid = $identities.ById[$target].ToString('D12', $culture)

    Start-DemoScheme 'EVSEB' 'Checking that the same batch verifies in EVSEB'
    $evseb = Invoke-EVSEBAudit $temp $identities.Ordinals 1
    if (-not $evseb.integrity -or $evseb.verified -ne $logIds.Count -or $evseb.requested -ne $logIds.Count) {
        throw "EVSEB could not verify the clean batch. $($evseb.note)"
    }
    $hashes = Read-CiphertextHashes $target $lid
    $state = @{
        Query = $request.Flags; Description = $request.Description; LogIds = $logIds
        LogId = $target; EvsebLid = $lid
        HmfBefore = $hashes[0]; HmfAfter = $hashes[1]
        EvsebBefore = $hashes[2]; EvsebAfter = $hashes[3]
    }
    $stateJson = ConvertTo-Json -InputObject $state -Depth 4
    $stream = [IO.File]::Open($statePath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
    try {
        $bytes = [Text.Encoding]::UTF8.GetBytes($stateJson)
        $stream.Write($bytes, 0, $bytes.Length)
        $stream.Flush($true)
    } finally {
        $stream.Dispose()
    }
    Switch-DemoCiphertexts $target $lid @($state.HmfBefore, $state.EvsebBefore)
    Write-Host "Changed one ciphertext byte for $target in HMF-Audit and EVSEB."
    Publish-DemoEvent 'tampered' @{ id = $target; message = 'Changed one ciphertext byte in HMF-Audit and EVSEB'; scheme = $null }
    exit 0
} catch {
    Write-Host "ERROR. $($_.Exception.Message)"
    Publish-DemoEvent 'error' @{ message = $_.Exception.Message }
    exit 2
} finally {
    Restore-GoEnvironment $saved
    Remove-DemoWorkspace $temp
}
