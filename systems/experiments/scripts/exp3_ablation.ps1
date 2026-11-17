# Experiment 3: HMF-Audit component ablation. Four variants (Full, no shared
# planning, no deduplication, no partition-aware retrieval) run over the same
# persisted fixture and the same requests, with the variant order rotated each
# round. -Warmup warm-up rounds and -Count measured rounds per variant and
# point. Raw CSV, summary CSV and the figure are written to
# <output>\exp3_ablation\.
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
    $results = Start-Experiment -Name "exp3_ablation" -Count $Count -Warmup $Warmup -BenchTime $BenchTime `
        -PerRegion $PerRegion -Dataset $Dataset -OutputDirectory $OutputDirectory
    Write-ExperimentEnvironment -Experiment "exp3_ablation" -Systems "hmf-audit" -Results $results
    $env:FAIR_ABLATION = "1"
    $env:FAIR_ABLATION_OUTPUT_DIR = $results
    $env:FAIR_ABLATION_WARMUP = [string]$Warmup
    $env:FAIR_ABLATION_MEASURED = [string]$Count
    Write-Note "ablation: $Warmup warm-up + $Count measured rounds per variant (q = 256, 1024, 4096, 16384)"
    Invoke-Native -What "the ablation" -Executable "go" -LogFile (Join-Path $results "ablation_run.log") `
        -WorkingDirectory (Join-Path $script:SystemsDir "hmf-audit") -Arguments @(
        "test", "./benchmarks/latency", "-run", "^TestFairHMFAblation$", "-v", "-count", "1", "-timeout", "0")
    Invoke-Native -What "experiment 3 post-processing" -Executable $script:Python -Arguments @(
        (Join-Path $script:ExpDir "postprocess.py"), "exp3", "--results-dir", $results, "--count", "$Count")
    Write-Note "experiment 3 finished: $results"
} finally {
    Restore-FairEnvironment
}
