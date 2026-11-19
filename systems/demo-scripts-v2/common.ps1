$ErrorActionPreference = 'Stop'
$systems = Split-Path $PSScriptRoot -Parent
$oldWork = Join-Path $systems 'demo-script/.work'
$dataset = Join-Path $systems 'experiments/datasets/spark_stratified_100000.jsonl'
$postgres = 'postgres://fair:fairdev@127.0.0.1:55432/fair_benchmark?sslmode=disable'
$concurrency = 8
$culture = [Globalization.CultureInfo]::InvariantCulture

function Publish-DemoEvent {
    param([string]$Type, [hashtable]$Fields = @{})
    if ($env:DEMO_EVENTS -ne '1') { return }
    $event = @{ type = $Type; scheme = $script:demoScheme }
    foreach ($key in $Fields.Keys) { $event[$key] = $Fields[$key] }
    [Console]::WriteLine('DEMO_EVENT ' + (ConvertTo-Json -InputObject $event -Compress -Depth 6))
}

function Start-DemoScheme {
    param([string]$Scheme, [string]$Text)
    $script:demoScheme = $Scheme
    Write-Host $Text
    Publish-DemoEvent 'scheme' @{ message = $Text }
}

function Invoke-Engine {
    param([string]$Program, [string[]]$Arguments, [string]$Directory = $PSScriptRoot)
    Push-Location -LiteralPath $Directory
    try {
        $ErrorActionPreference = 'Continue'
        $PSNativeCommandUseErrorActionPreference = $false
        $output = @(& $Program @Arguments 2>&1 | ForEach-Object {
            $line = [string]$_
            if ($line -match '^progress: (.+)$') {
                $message = $Matches[1]
                Write-Host "      $message"
                if ($message -match '^timed run (\d+)/(\d+)$') {
                    Publish-DemoEvent 'trial' @{ message = $message; current = [int]$Matches[1]; total = [int]$Matches[2] }
                } elseif ($message -match '^stored (\d+) records$') {
                    Publish-DemoEvent 'records' @{ message = $message; current = [long]$Matches[1] }
                } elseif ($message -match '^ALL authenticated (\d+) log ID\(s\) at checkpoint (\d+)$') {
                    Publish-DemoEvent 'locator' @{ message = $message; count = [int]$Matches[1]; checkpoint = [long]$Matches[2] }
                } else {
                    Publish-DemoEvent 'phase' @{ message = $message }
                }
            } else {
                if ($line -match '^\d{1,3}%\s+') {
                    Publish-DemoEvent 'records' @{ message = $line; percent = [int]([regex]::Match($line, '^\d+').Value) }
                }
                $line
            }
        })
        return [pscustomobject]@{ Code = $LASTEXITCODE; Lines = $output }
    } finally {
        Pop-Location
    }
}

function Assert-Exit {
    param($Result, [int[]]$Allowed = @(0))
    if ($Result.Code -notin $Allowed) {
        $details = ($Result.Lines | Select-Object -Last 8) -join [Environment]::NewLine
        throw "Engine exited with code $($Result.Code). $details"
    }
}

function Read-Report {
    param($Result)
    Assert-Exit $Result
    $json = $Result.Lines | Where-Object { $_.StartsWith('{') } | Select-Object -Last 1
    if (-not $json) { throw 'The engine did not produce a JSON result.' }
    return ConvertFrom-Json -InputObject $json
}

function Read-SourceIdentities {
    param([string[]]$LogIds, [switch]$IncludeBlasPositions, [switch]$IncludePositionIds)
    $wanted = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($id in $LogIds) {
        if (-not $wanted.Add($id)) { throw "ALL returned duplicate log ID $id." }
    }
    $source = [Collections.Generic.List[object]]::new()
    $byId = @{}
    $ordinal = 0
    foreach ($line in [IO.File]::ReadLines($dataset)) {
        $row = ConvertFrom-Json -InputObject $line
        if ($wanted.Remove([string]$row.logID)) { $byId[$row.logID] = $ordinal }
        if ($IncludeBlasPositions) {
            $eventTime = [DateTimeOffset]$row.event_time
            $source.Add([pscustomobject]@{ Id = [string]$row.logID; Ticks = $eventTime.UtcTicks; Ordinal = $ordinal })
        }
        $ordinal++
    }
    if ($ordinal -ne 100000) { throw "Expected 100,000 source rows in $dataset." }
    if ($wanted.Count -ne 0) { throw "$($wanted.Count) authenticated log ID(s) are missing from the source dataset." }

    # The existing BLAS demo stable-sorts by timestamp and writes files of 128 entries.
    $sorted = @($source | Sort-Object -Property Ticks, Ordinal)
    $positions = [Collections.Generic.HashSet[string]]::new()
    $positionIds = @{}
    for ($index = 0; $index -lt $sorted.Count; $index++) {
        $file = [Math]::Floor($index / 128)
        if ($IncludePositionIds) { $positionIds["$file/$($index % 128)"] = $sorted[$index].Id }
        if ($byId.ContainsKey($sorted[$index].Id)) {
            $null = $positions.Add("$file/$($index % 128)")
        }
    }
    return [pscustomobject]@{ Ordinals = @($LogIds | ForEach-Object { $byId[$_] }); ById = $byId; Positions = $positions; PositionIds = $positionIds }
}

function New-Result {
    param([string]$Scheme, [string]$Status, $Milliseconds = $null, [string]$Detail = '')
    $result = [pscustomobject]@{ Scheme = $Scheme; Status = $Status; MedianMs = $Milliseconds; Detail = $Detail }
    $timing = if ($null -ne $Milliseconds) { ' ({0:F3} ms)' -f $Milliseconds } else { '' }
    Write-Host "      $Status$timing"
    if ($Detail) { Write-Host "      $Detail" }
    Publish-DemoEvent 'result' @{ scheme = $Scheme; status = $Status; milliseconds = $Milliseconds; message = $Detail }
    return $result
}

function Get-DemoQuery {
    param($Overrides)
    $values = Import-PowerShellDataFile -Path (Join-Path $PSScriptRoot 'query.psd1')
    foreach ($name in @('Service', 'LogType', 'Region', 'Tenant', 'At', 'Start', 'End')) {
        if ($Overrides.ContainsKey($name)) { $values[$name] = $Overrides[$name] }
    }
    if ($Overrides.ContainsKey('At')) {
        if ($Overrides.ContainsKey('Start') -or $Overrides.ContainsKey('End')) { throw 'Use -At or -Start and -End.' }
        $values.Start = $values.End = ''
    }
    if (-not $values.Service -or -not $values.LogType -or -not $values.Region) {
        throw 'Service, log type and region are required.'
    }
    $parseTime = {
        param([string]$Value)
        if ($Value -cnotmatch '^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)$') {
            throw "Expected RFC 3339 time, received '$Value'."
        }
        return [DateTimeOffset]::Parse($Value, $culture)
    }
    if ($values.Start -or $values.End) {
        if (-not $values.Start -or -not $values.End) { throw 'Supply both -Start and -End.' }
        $lo = & $parseTime $values.Start
        $hi = & $parseTime $values.End
    } else {
        $lo = & $parseTime $values.At
        $hi = $lo.AddSeconds(1)
    }
    if ($lo -ge $hi) { throw 'Start must be before end.' }
    $from = $lo.ToUniversalTime().ToString('o', $culture)
    $to = $hi.ToUniversalTime().ToString('o', $culture)
    $flags = @('-service', $values.Service, '-log-type', $values.LogType, '-region', $values.Region, '-start', $from, '-end', $to)
    if ($values.Tenant -and $values.Tenant -ne '*') { $flags += @('-tenant', $values.Tenant) }
    return [pscustomobject]@{
        Flags = $flags
        Description = "[$from, $to) service=$($values.Service) log-type=$($values.LogType) region=$($values.Region) tenant=$($values.Tenant)"
    }
}

function Invoke-HMFAudit {
    param([string]$Binary, [string[]]$Query, [int]$Repeats, [switch]$Localize)
    Assert-Exit (Invoke-Engine go @('build', '-o', $Binary, './cmd/hmf-auditor') (Join-Path $systems 'hmf-audit'))
    $flags = @('-json', '-reuse-locator', '-repeats', $Repeats)
    if ($Localize) { $flags += '-localize' }
    $audit = Invoke-Engine $Binary ($flags + $Query)
    Assert-Exit $audit @(0, 1)
    $proof = $audit.Lines | Where-Object { $_ -match '^locator proof verified: \d+ matching' } | Select-Object -First 1
    if (-not $proof) {
        if ($audit.Code -ne 1) { throw 'HMF-Audit did not report an authenticated locator result.' }
        return [pscustomobject]@{ LocatorValid = $false; Code = $audit.Code }
    }
    $count = [int]([regex]::Match($proof, '\d+').Value)
    $rows = @($audit.Lines | Where-Object { $_.StartsWith('{') } | ForEach-Object { ConvertFrom-Json -InputObject $_ })
    if ($count -ne $rows.Count) { throw "HMF-Audit authenticated $count IDs but reported $($rows.Count) verdicts." }
    if ($env:DEMO_EVENTS -eq '1') {
        $items = @($rows | ForEach-Object { @{ id = $_.log_id; status = $_.status } })
        Publish-DemoEvent 'batch' @{ count = $count; rows = $items }
    }
    $median = $null
    if ($count -gt 0 -and $Repeats -gt 0) {
        $timing = $audit.Lines | Where-Object { $_ -match '^timing: median_ms=' } | Select-Object -First 1
        $median = [double]::Parse([regex]::Match($timing, 'median_ms=([0-9.]+)').Groups[1].Value, $culture)
    }
    return [pscustomobject]@{ LocatorValid = $true; Rows = $rows; Code = $audit.Code; MedianMs = $median }
}

function Invoke-EVSEBAudit {
    param([string]$Directory, [int[]]$Ordinals, [int]$Repeats, [switch]$Localize)
    $ordinalFile = Join-Path $Directory 'ordinals.txt'
    $reportFile = Join-Path $Directory 'evseb.json'
    [IO.File]::WriteAllLines($ordinalFile, [string[]]$Ordinals)
    $variables = @{
        FAIR_POSTGRES_DSN = $postgres
        FAIR_CASSANDRA_HOST = '127.0.0.1'
        FAIR_CASSANDRA_PORT = '19042'
        FAIR_DHT_PEERS = '16'
        FAIR_EVSEB_DHT_CONCURRENCY = [string]$concurrency
        DEMO_MODE = $(if ($Localize) { 'localize' } else { 'verify' })
        DEMO_ANCHOR = (Join-Path $oldWork 'evseb.anchor')
        DEMO_ORDINALS = $ordinalFile
        DEMO_OUT = $reportFile
        DEMO_REPEATS = [string]$Repeats
    }
    $saved = @{}
    foreach ($name in $variables.Keys) {
        $saved[$name] = [Environment]::GetEnvironmentVariable($name)
        [Environment]::SetEnvironmentVariable($name, $variables[$name])
    }
    try {
        Assert-Exit (Invoke-Engine go @('test', '-v', './internal/integrity', '-run', '^TestDemoEVSEB$', '-count=1', '-timeout', '30m') (Join-Path $systems 'EVBES'))
        return Get-Content -LiteralPath $reportFile -Raw | ConvertFrom-Json
    } finally {
        foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name]) }
    }
}

function New-DemoWorkspace {
    $path = Join-Path ([IO.Path]::GetTempPath()) ("hmf-demo-" + [Guid]::NewGuid())
    $null = New-Item -ItemType Directory -Path $path
    return $path
}

function Remove-DemoWorkspace {
    param([string]$Path)
    if (-not $Path) { return }
    foreach ($name in @('hmf-auditor.exe', 'blas-demo.exe', 'dht-demo.exe', 'ordinals.txt', 'evseb.json')) {
        $file = Join-Path $Path $name
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    Remove-Item -LiteralPath $Path
}

function Save-GoEnvironment {
    param([int]$GoCpus)
    $saved = @{
        CGO_ENABLED = $env:CGO_ENABLED
        GOMAXPROCS = $env:GOMAXPROCS
        DEMO_VERIFY_PROGRESS = $env:DEMO_VERIFY_PROGRESS
    }
    $env:CGO_ENABLED = '0'
    $env:GOMAXPROCS = [string]$GoCpus
    $env:DEMO_VERIFY_PROGRESS = '1'
    return $saved
}

function Restore-GoEnvironment {
    param($Saved)
    if ($null -eq $Saved) { return }
    foreach ($name in $Saved.Keys) { [Environment]::SetEnvironmentVariable($name, $Saved[$name]) }
}
