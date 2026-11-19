[CmdletBinding()]
param(
    [ValidateRange(1, 100)][int]$Repeats = 3,
    [ValidateRange(1, 256)][int]$GoCpus = 4
)

. (Join-Path $PSScriptRoot 'common.ps1')
$statePath = Join-Path $PSScriptRoot '.tamper-state.json'
$temp = $null
$saved = $null
try {
    if (-not (Test-Path -LiteralPath $statePath)) { throw 'Run tamper.ps1 to create a localization case first.' }
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    $temp = New-DemoWorkspace
    $saved = Save-GoEnvironment $GoCpus
    Write-Host "Query $($state.Description)"
    Write-Host "Expected log ID $($state.LogId)"
    Publish-DemoEvent 'target' @{ id = $state.LogId; message = 'Loaded the recorded tamper case' }
    Write-Host "Timing detection and localization over $Repeats run(s) after one warm-up per scheme."

    Start-DemoScheme 'HMF-Audit' '[1/2] HMF-Audit is building the localization engine'
    $audit = Invoke-HMFAudit (Join-Path $temp 'hmf-auditor.exe') $state.Query $Repeats -Localize
    if (-not $audit.LocatorValid) { throw 'HMF-Audit rejected the batch locator proof.' }
    $ids = @($audit.Rows | ForEach-Object { $_.log_id })
    if (($ids -join ',') -cne ($state.LogIds -join ',')) {
        throw 'The authenticated batch has changed since tamper.ps1 ran.'
    }
    $hmfFound = @($audit.Rows | Where-Object Status -eq 'REJECTED' | ForEach-Object { $_.log_id })
    $hmfStatus = if ($hmfFound.Count -eq 1 -and $hmfFound[0] -ceq $state.LogId -and $audit.Code -eq 1) { 'LOCALIZED' } else { 'REJECTED' }
    $hmfDetail = if ($hmfFound.Count) { "Reported log IDs $($hmfFound -join ', ')" } else { 'No tampered IDs reported.' }
    $results = @(New-Result 'HMF-Audit' $hmfStatus $audit.MedianMs $hmfDetail)

    Start-DemoScheme 'EVSEB' '[2/2] EVSEB is building the localization engine'
    $identities = Read-SourceIdentities $ids
    $expectedLid = $identities.ById[$state.LogId].ToString('D12', $culture)
    if ($expectedLid -cne $state.EvsebLid) { throw 'The source mapping for the target has changed.' }
    $report = Invoke-EVSEBAudit $temp $identities.Ordinals $Repeats -Localize
    $evsebFound = @($report.tampered | Where-Object { $_ })
    $evsebMatched = -not $report.integrity -and $report.requested -eq $ids.Count -and
        $report.verified -eq ($ids.Count - 1) -and $evsebFound.Count -eq 1 -and $evsebFound[0] -ceq $expectedLid
    $evsebStatus = if ($evsebMatched) { 'LOCALIZED' } else { 'REJECTED' }
    $evsebDetail = if ($report.note) {
        $report.note
    } elseif ($evsebFound.Count) {
        $byLid = @{}
        foreach ($id in $ids) { $byLid[$identities.ById[$id].ToString('D12', $culture)] = $id }
        $foundIds = $evsebFound | ForEach-Object { if ($byLid.ContainsKey($_)) { $byLid[$_] } else { "EVSEB LID $_" } }
        "Reported log IDs $($foundIds -join ', ')"
    } else {
        'No tampered IDs reported.'
    }
    $results += New-Result 'EVSEB' $evsebStatus $report.median_ms $evsebDetail

    $ranked = @($results | Sort-Object MedianMs)
    $ranked | Format-Table Scheme, Status, MedianMs -AutoSize | Out-Host
    if ($results.Status -contains 'REJECTED') { exit 1 }
    Write-Host ("Fastest localization: {0} ({1:F3} ms)" -f $ranked[0].Scheme, $ranked[0].MedianMs)
    exit 0
} catch {
    Write-Host "ERROR. $($_.Exception.Message)"
    Publish-DemoEvent 'error' @{ message = $_.Exception.Message }
    exit 2
} finally {
    Restore-GoEnvironment $saved
    Remove-DemoWorkspace $temp
}
