# Experiment 2: tamper detection and localization latency of HMF-Audit and
# EVSEB. One requested record (the middle one) is tampered in PostgreSQL;
# q = 1..4096 on clustered-2 and scattered requests. Raw output, trial CSV,
# summary CSV and Figure 2 are written to <output>\exp2_localization\.
[CmdletBinding()]
param(
    [ValidateRange(1, 1000)][int]$Count = 30,
    [ValidateRange(0, 1000000)][int]$Warmup = 5,
    [ValidateNotNullOrEmpty()][string]$BenchTime = "250ms",
    [ValidateRange(16384, 100000000)][int]$PerRegion = 2500000,
    [string]$Dataset = "",
    [string]$OutputDirectory = ""
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "lib.ps1")

try {
    $results = Start-Experiment -Name "exp2_localization" -Count $Count -Warmup $Warmup -BenchTime $BenchTime `
        -PerRegion $PerRegion -Dataset $Dataset -OutputDirectory $OutputDirectory
    Write-ExperimentEnvironment -Experiment "exp2_localization" -Systems "hmf-audit,evseb" -Results $results
    Invoke-FairBenchmark -Directory "hmf-audit" -Package "./benchmarks/latency" `
        -Pattern "^BenchmarkFairHMFLocalizationAfterMismatch$" -ResultFile (Join-Path $results "hmf-audit.txt")
    Invoke-FairBenchmark -Directory "EVBES" -Package "./internal/integrity" `
        -Pattern "^BenchmarkFairEVBESLocalizationAfterMismatch$" -ResultFile (Join-Path $results "evseb.txt")
    Invoke-Native -What "experiment 2 post-processing" -Executable $script:Python -Arguments @(
        (Join-Path $script:ExpDir "postprocess.py"), "exp2", "--results-dir", $results, "--dataset", $script:DatasetPath,
        "--count", "$Count", "--warmup", "$Warmup", "--bench-time", $BenchTime)
    Write-Note "experiment 2 finished: $results"
} finally {
    Restore-FairEnvironment
}
