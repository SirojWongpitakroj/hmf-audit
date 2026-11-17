# Ingest the deterministic subset into the local PostgreSQL and Cassandra for
# all four schemes. Exactly two sizes are accepted; any other number is
# rejected before anything is touched.
#
#   .\ingest.ps1 -Records 100000        generated from the source dataset (deterministic)
#   .\ingest.ps1 -Records 10000000      the existing 10M file (SHA-256 checked)
#   .\ingest.ps1 -Records 100000 -Systems hmf-audit,evseb
#
# Ingestion reuses the unchanged fair fixtures: each scheme's fixture seeds the
# database when its benchmark process starts, so this runs one minimal
# benchmark case (q=1, one execution, no warm-up) per scheme and exits. The
# data stays in the database afterwards. NOTE: the experiment scripts re-seed
# at their own start (the fixtures truncate first), so ingest.ps1 is for
# loading and inspecting the data, not a prerequisite of the experiments.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Records,
    [string]$Systems = "hmf-audit,dht-backed,blas,evseb"
)
$ErrorActionPreference = "Stop"

# Exact match on the canonical decimal text: 1e5, 100,000, 0100000, 100000.0
# and 10M are all rejected.
$allowed = @("100000", "10000000")
if ($allowed -cnotcontains $Records) {
    Write-Error "rejected -Records '$Records': only 100000 (100k) and 10000000 (10 million) are allowed" -ErrorAction Continue
    exit 2
}
$recordCount = [int]$Records

. (Join-Path $PSScriptRoot "lib.ps1")

$catalog = [ordered]@{
    "hmf-audit"  = @{ Directory = "hmf-audit";  Package = "./benchmarks/latency"; Pattern = '^BenchmarkFairHMFVerificationCore$/^clustered-2$/^q=1$' }
    "dht-backed" = @{ Directory = "dht-backed"; Package = "./benchmark";          Pattern = '^BenchmarkFairDHTVerificationRequest$/^clustered-2$/^q=1$' }
    "blas"       = @{ Directory = "blas";       Package = "./eval";               Pattern = '^BenchmarkFairBLASVerificationRequest$/^clustered-2$/^q=1$' }
    "evseb"      = @{ Directory = "EVBES";      Package = "./internal/integrity"; Pattern = '^BenchmarkFairEVBESVerificationRequest$/^clustered-2$/^q=1$' }
}
$selected = @($Systems -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" })
foreach ($system in $selected) {
    if (-not $catalog.Contains($system)) { throw "unknown system '$system'" }
}

try {
    $results = Start-Experiment -Name "ingest" -Count 1 -Warmup 0 -BenchTime "1x" `
        -PerRegion ($recordCount / $script:Regions) -Dataset "" -OutputDirectory ""
    Write-ExperimentEnvironment -Experiment "ingest" -Systems ($selected -join ",") -Results $results
    $started = Get-Date
    foreach ($system in $selected) {
        Write-Note "ingesting $system ($Records records)"
        $entry = $catalog[$system]
        Invoke-FairBenchmark -Directory $entry.Directory -Package $entry.Package -Pattern $entry.Pattern `
            -ResultFile (Join-Path $results "$system.txt")
    }

    # Report what is actually in the database, and fail if it does not match.
    Write-Note "row counts in PostgreSQL"
    $query = @"
SELECT table_schema || '.' || table_name || ' ' ||
       (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', table_schema, table_name), false, true, '')))[1]::text
FROM information_schema.tables
WHERE table_schema IN ('hmf','evseb','dht','blas') AND table_type = 'BASE TABLE'
ORDER BY 1;
"@
    $psql = @("exec", "-T", "postgres", "psql", "-q", "-At", "-U", "fair", "-d", "fair_benchmark", "-c", $query)
    $counts = & docker compose -f $script:ComposeFile @psql
    if ($LASTEXITCODE -ne 0) { throw "row-count query failed (exit $LASTEXITCODE)" }
    $counts | Set-Content -Encoding ascii -LiteralPath (Join-Path $results "row_counts.txt")
    $counts | ForEach-Object { Write-Note "  $_" }
    $tables = @{}
    foreach ($line in $counts) { $parts = "$line".Split(" "); if ($parts.Count -eq 2) { $tables[$parts[0]] = $parts[1] } }
    $expected = @{ "hmf-audit" = "hmf.encrypted_logs"; "evseb" = "evseb.encrypted_logs" }
    foreach ($system in $selected) {
        if (-not $expected.ContainsKey($system)) { continue }
        $table = $expected[$system]
        if ($tables[$table] -ne "$Records") { throw "$table has '$($tables[$table])' rows, expected $Records" }
    }
    Write-Note ("ingestion of $Records records finished in {0:N0} s; logs in $results" -f ((Get-Date) - $started).TotalSeconds)
} finally {
    Restore-FairEnvironment
}
