# Experiment 1: verification latency of HMF-Audit, DHT-backed, BLAS and EVSEB
# on clustered-2 and scattered requests of q = 1..4096 records, over honest
# storage. Raw `go test -bench` output, trial CSV, summary CSV and Figure 1
# are written to <output>\exp1_verification\.
[CmdletBinding()]
param(
    [ValidateRange(1, 1000)][int]$Count = 30,
    [ValidateRange(0, 1000000)][int]$Warmup = 5,
    [ValidateNotNullOrEmpty()][string]$BenchTime = "250ms",
    [ValidateRange(16384, 100000000)][int]$PerRegion = 2500000,
    [string]$Dataset = "",
    [string]$OutputDirectory = "",
    [string]$Systems = "hmf-audit,dht-backed,blas,evseb"
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "lib.ps1")

$catalog = [ordered]@{
    "hmf-audit"  = @{ Directory = "hmf-audit";  Package = "./benchmarks/latency"; Pattern = "^BenchmarkFairHMFVerificationCore$" }
    "dht-backed" = @{ Directory = "dht-backed"; Package = "./benchmark";          Pattern = "^BenchmarkFairDHTVerificationRequest$" }
    "blas"       = @{ Directory = "blas";       Package = "./eval";               Pattern = "^BenchmarkFairBLASVerificationRequest$" }
    "evseb"      = @{ Directory = "EVBES";      Package = "./internal/integrity"; Pattern = "^BenchmarkFairEVBESVerificationRequest$" }
}
$selected = @($Systems -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" })
foreach ($system in $selected) {
    if (-not $catalog.Contains($system)) { throw "unknown system '$system'" }
}

try {
    $results = Start-Experiment -Name "exp1_verification" -Count $Count -Warmup $Warmup -BenchTime $BenchTime `
        -PerRegion $PerRegion -Dataset $Dataset -OutputDirectory $OutputDirectory
    Write-ExperimentEnvironment -Experiment "exp1_verification" -Systems ($selected -join ",") -Results $results
    foreach ($system in $selected) {
        $entry = $catalog[$system]
        Invoke-FairBenchmark -Directory $entry.Directory -Package $entry.Package -Pattern $entry.Pattern `
            -ResultFile (Join-Path $results "$system.txt")
    }
    Invoke-Native -What "experiment 1 post-processing" -Executable $script:Python -Arguments @(
        (Join-Path $script:ExpDir "postprocess.py"), "exp1", "--results-dir", $results, "--dataset", $script:DatasetPath,
        "--count", "$Count", "--warmup", "$Warmup", "--bench-time", $BenchTime, "--systems", ($selected -join ","))
    Write-Note "experiment 1 finished: $results"
} finally {
    Restore-FairEnvironment
}
