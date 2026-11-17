# Runner: runs the three experiment scripts one after another against the
# local Docker stack, then (for the existing 10M subset) compares the new
# results with the earlier 10M results.
#
# Each experiment re-seeds its own fixtures, exactly as the original fair
# benchmark does. A failing experiment stops the run.
[CmdletBinding()]
param(
    [ValidateRange(1, 1000)][int]$Count = 30,
    [ValidateRange(0, 1000000)][int]$Warmup = 5,
    [ValidateNotNullOrEmpty()][string]$BenchTime = "250ms",
    [ValidateRange(16384, 100000000)][int]$PerRegion = 2500000,
    [string]$Dataset = "",
    [string]$OutputDirectory = "",
    [string]$Systems = "hmf-audit,dht-backed,blas,evseb",
    [switch]$NoCompare
)
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "lib.ps1")

Set-Settings -Count $Count -Warmup $Warmup -BenchTime $BenchTime -PerRegion $PerRegion -Dataset $Dataset -OutputDirectory $OutputDirectory
$script:Python = Find-Python
foreach ($tool in @("go", "docker")) {
    if ($null -eq (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed" }
}
Confirm-Dataset
Start-Services

# Hand the verified dataset and the running services to the three scripts.
$common = @{
    Count = $Count; Warmup = $Warmup; BenchTime = $BenchTime; PerRegion = $PerRegion
    Dataset = $script:DatasetPath; OutputDirectory = $script:OutputPath
}
$started = Get-Date
Write-Note "=== exp1_verification ==="
& (Join-Path $PSScriptRoot "exp1_verification.ps1") @common -Systems $Systems
Write-Note "=== exp2_localization ==="
& (Join-Path $PSScriptRoot "exp2_localization.ps1") @common
Write-Note "=== exp3_ablation ==="
& (Join-Path $PSScriptRoot "exp3_ablation.ps1") @common

if (-not $NoCompare -and $script:DatasetPath -eq $script:Existing10mFile -and (Test-Path -LiteralPath $script:PreviousSummary)) {
    Write-Note "=== comparison with the earlier 10M results (HMF-Audit and EVSEB points) ==="
    foreach ($experiment in @("exp1_verification", "exp2_localization")) {
        Write-Host "--- $experiment"
        $previous = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        & $script:Python (Join-Path $script:ExpDir "postprocess.py") compare --old $script:PreviousSummary `
            --new (Join-Path $script:OutputPath "$experiment\latency_summary.csv")
        $ErrorActionPreference = $previous
    }
}
Write-Note ("all three experiments finished in {0:N0} s; results in {1}" -f ((Get-Date) - $started).TotalSeconds, $script:OutputPath)
