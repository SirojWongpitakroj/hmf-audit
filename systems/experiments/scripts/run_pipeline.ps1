# One command for the whole 10M-record study, from the public logs to the figures:
#
#   1. preflight   - tools, Python + matplotlib, Docker (fail now, not after a day)
#   2. dataset     - download the Loghub Spark logs (33,236,604 lines) and deterministically
#                    derive the 10,000,000-record subset (dataset\scripts\prepare_spark_10m.py).
#                    Already-present, verified data is not downloaded or rebuilt again.
#                    With -Records 100000 the combined file is prepared and the 100k subset
#                    is cut from it by the experiment scripts.
#   3. ingest      - load the subset into PostgreSQL + Cassandra (ingest.ps1)
#   4. experiments - exp1 verification, exp2 localization, exp3 ablation (run_all.ps1)
#   5. figures     - re-plot Figures 1-3 with Python from the raw results, check that all
#                    three PNGs exist and copy them to <output>\figures\
#
#   .\run_pipeline.ps1 [-Records 10000000|100000] [-Systems LIST] [-Count N] [-Warmup N] [-BenchTime T]
#                       [-SkipIngest] [-NoCompare]
#
# -Records defaults to 10000000. 100000 runs the identical pipeline on the
# deterministic 100k subset (a dry run before committing to the 10M run).
#
# The default systems for experiment 1 are hmf-audit,blas,evseb: dht-backed is
# rejected by its own memory bounds at q = 4096 and 16384 (see README.md, "Known
# limitations"), which would stop the run. Add it back with
# -Systems hmf-audit,dht-backed,blas,evseb if that has been resolved.
#
# NOTE: the experiment scripts re-seed the databases when each one starts (the
# fixtures truncate first), so step 3 is repeated work at 10M. It is on by
# default because it was asked for; -SkipIngest drops it and the results are
# identical.
#
# Output: systems\experiments\results\spark-<records>\ and a transcript of this run in
# systems\experiments\results\pipeline-<records>-<timestamp>.log
[CmdletBinding()]
param(
    [ValidateRange(1, 1000)][int]$Count = 30,
    [ValidateRange(0, 1000000)][int]$Warmup = 5,
    [ValidateNotNullOrEmpty()][string]$BenchTime = "250ms",
    [string]$Records = "10000000",
    [string]$Systems = "hmf-audit,blas,evseb",
    [switch]$SkipIngest,
    [switch]$NoCompare
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "lib.ps1")

# Exact decimal text only (1e5, 100,000, 10M are rejected), as in ingest.ps1.
if (@("100000", "10000000") -cnotcontains $Records) { throw "-Records must be 10000000 or 100000, got '$Records'" }
$resultsRoot = Join-Path $script:ExpDir "results"
New-Item -ItemType Directory -Force -Path $resultsRoot | Out-Null
$log = Join-Path $resultsRoot ("pipeline-$Records-{0}.log" -f (Get-Date -Format "yyyyMMdd-HHmmss"))
Start-Transcript -LiteralPath $log | Out-Null
try {
    Write-Note "log of this run: $log"
    $started = Get-Date

    # ------------------------------------------------------------ 1. preflight
    Write-Note "=== 1/5 preflight ==="
    Set-Settings -Count $Count -Warmup $Warmup -BenchTime $BenchTime -PerRegion ([int]$Records / $script:Regions) -Dataset "" -OutputDirectory ""
    if ($script:RecordCount -ne [int]$Records) { throw "internal: expected $Records records, got $($script:RecordCount)" }
    $script:Python = Find-Python
    $previous = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    & $script:Python -c "import matplotlib" 2>&1 | Out-Null
    $hasMatplotlib = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = $previous
    if (-not $hasMatplotlib) {
        throw "python module 'matplotlib' is missing; install it first ($($script:Python) -m pip install matplotlib) - the figures are the last step of a very long run"
    }
    foreach ($tool in @("go", "docker")) {
        if ($null -eq (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed" }
    }
    $ErrorActionPreference = "Continue"
    & docker info 2>&1 | Out-Null
    $dockerUp = ($LASTEXITCODE -eq 0)
    $ErrorActionPreference = $previous
    if (-not $dockerUp) { throw "the docker daemon is not running" }
    if ($Records -eq "10000000") { Write-Note "this run needs roughly 35 GB free for Docker plus about 25 GB for the dataset (README.md, 'Cost') and takes well over a day" }

    # ------------------------------------------------------------- 2. dataset
    # Download Loghub Spark and derive the subset. For 10M the result is the exact
    # file the pipeline expects (SHA-256 pinned); for 100k only the combined file
    # is prepared and Confirm-Dataset cuts the 100k subset from it.
    $stopAfter = if ($Records -eq "10000000") { "subset" } else { "combined" }
    Write-Note "=== 2/5 dataset: download Loghub Spark, derive the subset ($stopAfter) ==="
    Invoke-Native -What "dataset preparation" -Executable $script:Python -Arguments @(
        (Join-Path $script:RepoDir "dataset\scripts\prepare_spark_10m.py"), "--stop-after", $stopAfter)
    Confirm-Dataset   # file exists, has the expected records, SHA-256 matches (10M)

    # --------------------------------------------------------------- 3. ingest
    if ($SkipIngest) {
        Write-Note "=== 3/5 ingest skipped (-SkipIngest); the experiments seed the data themselves ==="
    } else {
        Write-Note "=== 3/5 ingest $Records records ==="
        & (Join-Path $PSScriptRoot "ingest.ps1") -Records $Records -Systems $Systems
    }

    # ----------------------------------------------------------- 4. experiments
    Write-Note "=== 4/5 experiments 1, 2, 3 ==="
    $runAll = @{ Count = $Count; Warmup = $Warmup; BenchTime = $BenchTime; Systems = $Systems; PerRegion = ([int]$Records / $script:Regions) }
    if ($NoCompare) { $runAll["NoCompare"] = $true }
    & (Join-Path $PSScriptRoot "run_all.ps1") @runAll

    # --------------------------------------------------------------- 5. figures
    Write-Note "=== 5/5 figures ==="
    $post = Join-Path $script:ExpDir "postprocess.py"
    $r1 = Join-Path $script:OutputPath "exp1_verification"
    $r2 = Join-Path $script:OutputPath "exp2_localization"
    $r3 = Join-Path $script:OutputPath "exp3_ablation"
    Invoke-Native -What "figure 1" -Executable $script:Python -Arguments @($post, "exp1", "--results-dir", $r1,
        "--dataset", $script:DatasetPath, "--count", "$Count", "--warmup", "$Warmup", "--bench-time", $BenchTime, "--systems", $Systems)
    Invoke-Native -What "figure 2" -Executable $script:Python -Arguments @($post, "exp2", "--results-dir", $r2,
        "--dataset", $script:DatasetPath, "--count", "$Count", "--warmup", "$Warmup", "--bench-time", $BenchTime)
    Invoke-Native -What "figure 3" -Executable $script:Python -Arguments @($post, "exp3", "--results-dir", $r3, "--count", "$Count")

    $figures = Join-Path $script:OutputPath "figures"
    New-Item -ItemType Directory -Force -Path $figures | Out-Null
    foreach ($figure in @((Join-Path $r1 "figure1_verification_latency.png"),
                          (Join-Path $r2 "figure2_localization_latency.png"),
                          (Join-Path $r3 "figure3_ablation_latency.png"))) {
        if (-not (Test-Path -LiteralPath $figure) -or (Get-Item -LiteralPath $figure).Length -eq 0) {
            throw "expected figure was not produced: $figure"
        }
        Copy-Item -LiteralPath $figure -Destination $figures -Force
    }
    Write-Note ("pipeline finished in {0:N0} min" -f ((Get-Date) - $started).TotalMinutes)
    Write-Note "figures: $figures"
    Get-ChildItem -LiteralPath $figures | ForEach-Object { Write-Note "  $($_.Name)" }
} finally {
    Stop-Transcript | Out-Null
}
