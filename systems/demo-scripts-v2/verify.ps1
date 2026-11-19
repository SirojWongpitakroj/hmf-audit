# Edit query.psd1 or supply query flags.
[CmdletBinding()]
param(
    [string]$Tenant,
    [string]$Service,
    [string]$LogType,
    [string]$Region,
    [string]$At,
    [string]$Start,
    [string]$End,
    [ValidateRange(1, 100)][int]$Repeats = 3,
    [ValidateRange(1, 256)][int]$GoCpus = 4
)

. (Join-Path $PSScriptRoot "common.ps1")

$temp = $null
$saved = $null
try {
    $request = Get-DemoQuery $PSBoundParameters
    $query = $request.Flags
    $null = Get-Command go
    $temp = New-DemoWorkspace
    $saved = Save-GoEnvironment $GoCpus
    $hmf = Join-Path $temp 'hmf-auditor.exe'
    $blas = Join-Path $temp 'blas-demo.exe'
    $dht = Join-Path $temp 'dht-demo.exe'
    $ordinals = Join-Path $temp 'ordinals.txt'

    Write-Host "Query $($request.Description)"
    Write-Host "Timing $Repeats run(s) per scheme after one warm-up. ALL lookup and build time are excluded."
    Start-DemoScheme 'HMF-Audit' '[1/4] HMF-Audit is building the auditor'
    $audit = Invoke-HMFAudit $hmf $query $Repeats
    if (-not $audit.LocatorValid) {
        Write-Host 'REJECTED. The ALL proof failed. The other schemes have no authenticated batch to verify.'
        $null = New-Result 'HMF-Audit' 'REJECTED' -Detail 'The ALL proof failed.'
        exit 1
    }
    $count = $audit.Rows.Count
    if ($count -eq 0) {
        Write-Host 'VALID. The ALL proof authenticated an empty result. No log verification was timed.'
        $null = New-Result 'HMF-Audit' 'VALID' -Detail 'The ALL proof authenticated an empty result.'
        exit 0
    }
    $logIds = @($audit.Rows | ForEach-Object { $_.log_id })
    Write-Host "Authenticated log IDs ($count)"
    foreach ($row in $audit.Rows) { Write-Host "      $($row.log_id)  $($row.status)" }
    $hmfStatus = if ($audit.Code -eq 0) { 'VALID' } else { 'REJECTED' }
    $results = @(New-Result 'HMF-Audit' $hmfStatus $audit.MedianMs)

    Write-Host 'Mapping authenticated IDs to baseline record identifiers'
    Publish-DemoEvent 'phase' @{ message = 'Mapping authenticated IDs to baseline record identifiers'; scheme = $null }
    $identities = Read-SourceIdentities $logIds -IncludeBlasPositions
    [IO.File]::WriteAllLines($ordinals, [string[]]$identities.Ordinals)

    Start-DemoScheme 'BLAS' '[2/4] BLAS is building the verifier'
    try {
        Assert-Exit (Invoke-Engine go @('build', '-o', $blas, './cmd/demo') (Join-Path $systems 'blas'))
        $report = Read-Report (Invoke-Engine $blas (@('-mode', 'query', '-positions', '-dsn', $postgres,
            '-repeats', $Repeats, '-concurrency', $concurrency) + $query))
        $actual = [Collections.Generic.HashSet[string]]::new()
        foreach ($position in $report.positions) { $null = $actual.Add("$($position[0])/$($position[1])") }
        $same = $report.entries -eq $count -and $actual.SetEquals($identities.Positions)
        if ($report.accept -and $same) {
            $results += New-Result 'BLAS' 'VALID' $report.median_ms
        } else {
            $detail = if ($report.accept) { 'The verified BLAS entries differ from the ALL result.' } else { 'The BLAS proof failed.' }
            $results += New-Result 'BLAS' 'REJECTED' $report.median_ms $detail
        }
    } catch {
        $results += New-Result 'BLAS' 'ERROR' -Detail $_.Exception.Message
    }

    Start-DemoScheme 'DHT-backed' '[3/4] DHT-backed is building the verifier'
    try {
        Assert-Exit (Invoke-Engine go @('build', '-o', $dht, './benchmark/cmd/demo') (Join-Path $systems 'dht-backed'))
        $report = Read-Report (Invoke-Engine $dht @('-mode', 'verify', '-ordinals', $ordinals,
            '-pg-dsn', "$postgres&search_path=dht", '-cassandra-host', '127.0.0.1', '-cassandra-port', '19042',
            '-work-dir', (Join-Path $oldWork 'dht-work'), '-key-dir', (Join-Path $oldWork 'dht-keys'),
            '-peers', '16', '-concurrency', $concurrency, '-repeats', $Repeats))
        $status = if ($report.accept -and $report.requested -eq $count) { 'VALID' } else { 'REJECTED' }
        $results += New-Result 'DHT-backed' $status $report.median_ms
    } catch {
        $results += New-Result 'DHT-backed' 'ERROR' -Detail $_.Exception.Message
    }

    Start-DemoScheme 'EVSEB' '[4/4] EVSEB is building the verifier'
    try {
        $report = Invoke-EVSEBAudit $temp $identities.Ordinals $Repeats
        $status = if ($report.integrity -and $report.verified -eq $count -and $report.requested -eq $count) { 'VALID' } else { 'REJECTED' }
        $results += New-Result 'EVSEB' $status $report.median_ms $report.note
    } catch {
        $results += New-Result 'EVSEB' 'ERROR' -Detail $_.Exception.Message
    }

    $results | Sort-Object { if ($null -eq $_.MedianMs) { [double]::PositiveInfinity } else { $_.MedianMs } } |
        Format-Table Scheme, Status, MedianMs -AutoSize | Out-Host
    $valid = @($results | Where-Object Status -eq 'VALID' | Sort-Object MedianMs)
    if ($valid.Count -ge 2) {
        Write-Host ("Fastest valid verification of {0}: {1} ({2:F3} ms)" -f $valid.Count, $valid[0].Scheme, $valid[0].MedianMs)
    }
    if ($results.Status -contains 'ERROR') { exit 2 }
    if ($results.Status -contains 'REJECTED') { exit 1 }
    exit 0
} catch {
    Write-Host "ERROR. $($_.Exception.Message)"
    Publish-DemoEvent 'error' @{ message = $_.Exception.Message }
    exit 2
} finally {
    Restore-GoEnvironment $saved
    Remove-DemoWorkspace $temp
}
