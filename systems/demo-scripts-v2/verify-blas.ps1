# Use BLAS's native attribute query as the source of the batch verified by the other schemes.
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

. (Join-Path $PSScriptRoot 'common.ps1')

$temp = $null
$saved = $null
$results = @()
try {
    $request = Get-DemoQuery $PSBoundParameters
    $query = $request.Flags
    $null = Get-Command go
    $temp = New-DemoWorkspace
    $saved = Save-GoEnvironment $GoCpus
    $blas = Join-Path $temp 'blas-demo.exe'
    $hmf = Join-Path $temp 'hmf-auditor.exe'
    $dht = Join-Path $temp 'dht-demo.exe'
    $ordinals = Join-Path $temp 'ordinals.txt'

    Write-Host "Query $($request.Description)"
    Write-Host "BLAS selects and publishes the batch; the other schemes verify those log IDs."
    Write-Host "Timing $Repeats run(s) per scheme after one warm-up. Build and ID mapping time are excluded."

    Start-DemoScheme 'BLAS' '[1/4] BLAS is resolving and verifying the attribute query'
    Assert-Exit (Invoke-Engine go @('build', '-o', $blas, './cmd/demo') (Join-Path $systems 'blas'))
    $blasReport = Read-Report (Invoke-Engine $blas (@('-mode', 'query', '-positions', '-dsn', $postgres,
        '-repeats', $Repeats, '-concurrency', $concurrency) + $query))
    if (-not $blasReport.accept) {
        $results += New-Result 'BLAS' 'REJECTED' $blasReport.median_ms 'The native BLAS query proof failed.'
        exit 1
    }

    Write-Host 'Mapping BLAS positions to source log IDs (excluded from all scheme timings)'
    Publish-DemoEvent 'phase' @{ message = 'Mapping BLAS positions to source log IDs; excluded from scheme timings.'; scheme = $null }
    $mappingTimer = [Diagnostics.Stopwatch]::StartNew()
    $allIdentities = Read-SourceIdentities @() -IncludeBlasPositions -IncludePositionIds
    $logIds = [Collections.Generic.List[string]]::new()
    $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($position in $blasReport.positions) {
        $key = "$($position[0])/$($position[1])"
        if (-not $allIdentities.PositionIds.ContainsKey($key)) { throw "BLAS returned an unknown source position $key." }
        $id = [string]$allIdentities.PositionIds[$key]
        if (-not $seen.Add($id)) { throw "BLAS returned duplicate log ID $id." }
        $logIds.Add($id)
    }
    if ($logIds.Count -ne $blasReport.entries) { throw 'BLAS entry count did not match its published log IDs.' }
    $mappingTimer.Stop()
    Write-Host ('      ID mapping completed in {0:F3} ms (excluded from BLAS and verifier timings).' -f $mappingTimer.Elapsed.TotalMilliseconds)
    Publish-DemoEvent 'phase' @{ message = ('ID mapping completed in {0:F3} ms; excluded from scheme timings.' -f $mappingTimer.Elapsed.TotalMilliseconds); scheme = $null }
    $count = $logIds.Count
    $results += New-Result 'BLAS' 'VALID' $blasReport.median_ms "BLAS published $count verified log ID(s)."
    Write-Host "BLAS published log IDs ($count)"
    foreach ($id in $logIds) { Write-Host "      $id" }
    Publish-DemoEvent 'batch' @{ count = $count; source = 'BLAS'; rows = @($logIds | ForEach-Object { @{ id = $_; status = 'VALID' } }) }

    Start-DemoScheme 'HMF-Audit' '[2/4] HMF-Audit is verifying the BLAS-selected query results'
    $audit = Invoke-HMFAudit $hmf $query $Repeats
    if (-not $audit.LocatorValid) {
        $results += New-Result 'HMF-Audit' 'REJECTED' -Detail 'The ALL proof failed; the BLAS batch could not be matched to authenticated IDs.'
        exit 1
    }
    $hmfIds = @($audit.Rows | ForEach-Object { [string]$_.log_id })
    $hmfSet = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($id in $hmfIds) { $null = $hmfSet.Add($id) }
    $sameIds = $hmfIds.Count -eq $count -and $hmfSet.SetEquals($seen)
    $hmfStatus = if ($audit.Code -eq 0 -and $sameIds) { 'VALID' } else { 'REJECTED' }
    $hmfDetail = if ($sameIds) { 'Authenticated the same log IDs published by BLAS.' } else { 'The authenticated HMF-Audit result differs from the BLAS-published IDs.' }
    $results += New-Result 'HMF-Audit' $hmfStatus $audit.MedianMs $hmfDetail
    if (-not $sameIds) { exit 1 }

    if ($count -eq 0) {
        $results += New-Result 'DHT-backed' 'VALID' -Detail 'BLAS published no matching log IDs; no records needed verification.'
        $results += New-Result 'EVSEB' 'VALID' -Detail 'BLAS published no matching log IDs; no records needed verification.'
    } else {
        $identities = Read-SourceIdentities $logIds.ToArray()
        [IO.File]::WriteAllLines($ordinals, [string[]]$identities.Ordinals)

        Start-DemoScheme 'DHT-backed' '[3/4] DHT-backed is verifying the BLAS-published log IDs'
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

        Start-DemoScheme 'EVSEB' '[4/4] EVSEB is verifying the BLAS-published log IDs'
        try {
            $report = Invoke-EVSEBAudit $temp $identities.Ordinals $Repeats
            $status = if ($report.integrity -and $report.verified -eq $count -and $report.requested -eq $count) { 'VALID' } else { 'REJECTED' }
            $results += New-Result 'EVSEB' $status $report.median_ms $report.note
        } catch {
            $results += New-Result 'EVSEB' 'ERROR' -Detail $_.Exception.Message
        }
    }

    $results | Sort-Object { if ($null -eq $_.MedianMs) { [double]::PositiveInfinity } else { $_.MedianMs } } |
        Format-Table Scheme, Status, MedianMs -AutoSize | Out-Host
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
