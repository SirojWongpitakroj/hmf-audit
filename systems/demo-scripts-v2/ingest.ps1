[CmdletBinding()]
param(
    [ValidateSet('all', 'HMF-Audit', 'BLAS', 'DHT-backed', 'EVSEB')][string]$Scheme = 'all',
    [switch]$Replace
)

. (Join-Path $PSScriptRoot 'common.ps1')
$saved = $null
$previousProgress = $env:DEMO_INGEST_PROGRESS
try {
    if (-not $Replace) { throw 'Supply -Replace to replace the selected local demo data.' }
    if (Test-Path -LiteralPath (Join-Path $PSScriptRoot '.tamper-state.json')) {
        throw 'Restore the active tamper case before re-ingesting.'
    }
    $count = 0
    foreach ($line in [IO.File]::ReadLines($dataset)) { $count++ }
    if ($count -ne 100000) { throw "The demo needs the 100,000-record source file at $dataset." }
    $saved = Save-GoEnvironment 4
    $env:DEMO_INGEST_PROGRESS = '1'
    $env:DEMO_MODE = 'ingest'
    $env:DEMO_ANCHOR = Join-Path $oldWork 'evseb.anchor'
    $env:DEMO_OUT = Join-Path $oldWork 'evseb-ingest.json'
    $env:FAIR_DATASET_PATH = $dataset
    $env:FAIR_POSTGRES_DSN = $postgres
    $env:FAIR_CASSANDRA_HOST = '127.0.0.1'
    $env:FAIR_CASSANDRA_PORT = '19042'
    $env:FAIR_DHT_PEERS = '16'
    $env:FAIR_EVSEB_DHT_CONCURRENCY = '8'
    $null = New-Item -ItemType Directory -Path $oldWork -Force
    $engine = @{
        'HMF-Audit' = @{
            directory = 'hmf-audit'
            args = @('run', './cmd/hmf-ingest', '-dataset', $dataset, '-key-file',
                (Join-Path $systems 'attribute-query/keys/hmf-ingest.key'), '-reset')
        }
        'BLAS' = @{
            directory = 'blas'
            args = @('run', './cmd/demo', '-mode', 'ingest', '-dataset', $dataset, '-dsn', $postgres)
        }
        'DHT-backed' = @{
            directory = 'dht-backed'
            args = @('run', './benchmark/cmd/demo', '-mode', 'ingest', '-dataset', $dataset,
                '-pg-dsn', "$postgres&search_path=dht", '-cassandra-host', '127.0.0.1',
                '-work-dir', (Join-Path $oldWork 'dht-work'), '-key-dir', (Join-Path $oldWork 'dht-keys'))
        }
        'EVSEB' = @{
            directory = 'EVBES'
            args = @('test', '-v', './internal/integrity', '-run', '^TestDemoEVSEB$', '-count=1', '-timeout', '30m')
        }
    }
    $order = if ($Scheme -eq 'all') { @('HMF-Audit', 'BLAS', 'DHT-backed', 'EVSEB') } else { @($Scheme) }
    $failed = $false
    foreach ($name in $order) {
        Start-DemoScheme $name "Ingesting $name"
        $started = [Diagnostics.Stopwatch]::StartNew()
        try {
            $config = $engine[$name]
            $run = Invoke-Engine go $config.args (Join-Path $systems $config.directory)
            Assert-Exit $run
            $run.Lines | Select-Object -Last 6 | ForEach-Object { Write-Host $_ }
            Publish-DemoEvent 'records' @{ current = $count }
            $null = New-Result $name 'INGESTED' -Detail 'Proof state finalized'
        } catch {
            $failed = $true
            $null = New-Result $name 'ERROR' -Detail $_.Exception.Message
        } finally {
            Publish-DemoEvent 'elapsed' @{ milliseconds = $started.Elapsed.TotalMilliseconds }
        }
    }
    if ($failed) { exit 2 }
    exit 0
} catch {
    Write-Host "ERROR. $($_.Exception.Message)"
    Publish-DemoEvent 'error' @{ message = $_.Exception.Message }
    exit 2
} finally {
    $env:DEMO_INGEST_PROGRESS = $previousProgress
    Restore-GoEnvironment $saved
}
