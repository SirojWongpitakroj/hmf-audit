# Shared helpers for the local experiment scripts. Dot-source this file; do not run it.
#
# Everything here targets the local Docker stack in systems/fair-persistence
# (PostgreSQL on 127.0.0.1:55432, Cassandra on 127.0.0.1:19042). The endpoints
# are fixed on purpose: these scripts cannot be pointed at a remote database.

Set-StrictMode -Version Latest

$script:ScriptsDir = $PSScriptRoot
$script:ExpDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path   # systems\experiments: results\, datasets\, postprocess.py, *.sql
$script:SystemsDir = (Resolve-Path (Join-Path $script:ExpDir "..")).Path
$script:RepoDir = (Resolve-Path (Join-Path $script:SystemsDir "..")).Path

# ---- fixed protocol
$script:Regions = 4
$script:ShardsPerRegion = 1
$script:SegmentLeaves = 8192
$script:HppConcurrency = 8
$script:EvsebDhtConcurrency = 8
$script:DhtConcurrency = 8
$script:DhtPeers = 1
$script:DefaultPerRegion = 2500000
# SHA-256 of the existing 10,000,000-record subset used by the earlier results.
$script:Expected10mSha256 = "a8fa53db3327ce5b21cc61b249276817d81b2ee29a6facf64f48619d215dba82"
$script:Existing10mFile = Join-Path $script:SystemsDir "fair-results-10m-localization\spark_stratified_10000000.jsonl"
$script:SourceDataset = Join-Path $script:RepoDir "dataset\combined\spark_uniform_combined.jsonl"
$script:PreviousSummary = Join-Path $script:SystemsDir "fair-results-10m-localization\latency_summary.csv"
$script:ComposeFile = Join-Path $script:SystemsDir "fair-persistence\docker-compose.yml"

function Write-Note {
    param([string]$Message)
    Write-Host ("[{0}] {1}" -f (Get-Date -Format "HH:mm:ss"), $Message)
}

function Find-Python {
    foreach ($candidate in @("python", "python3", "py")) {
        $command = Get-Command $candidate -ErrorAction SilentlyContinue
        if ($null -eq $command) { continue }
        $previous = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        & $command.Source -c "import sys; sys.exit(0 if sys.version_info >= (3, 9) else 1)" 2>&1 | Out-Null
        $ok = ($LASTEXITCODE -eq 0)
        $ErrorActionPreference = $previous
        if ($ok) { return $command.Source }
    }
    throw "python 3.9 or newer is required"
}

# Run a native command, stream its output to the console and (optionally) to a
# UTF-8 log file, and fail on a non-zero exit code. Native stderr lines must not
# become terminating PowerShell errors, so errors are relaxed around the call.
function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$What,
        [Parameter(Mandatory = $true)][string]$Executable,
        [string[]]$Arguments = @(),
        [string]$LogFile = "",
        [string]$StdinText = $null,
        [string]$WorkingDirectory = ""
    )
    $previous = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $writer = $null
    if ($LogFile -ne "") {
        $writer = New-Object System.IO.StreamWriter($LogFile, $false, (New-Object System.Text.UTF8Encoding($false)))
    }
    if ($WorkingDirectory -ne "") { Push-Location $WorkingDirectory }
    try {
        if ($null -ne $StdinText) {
            $lines = $StdinText | & $Executable @Arguments 2>&1
        } else {
            $lines = & $Executable @Arguments 2>&1
        }
        foreach ($line in $lines) {
            $text = "$line"
            Write-Host $text
            if ($null -ne $writer) { $writer.WriteLine($text) }
        }
        $exitCode = $LASTEXITCODE
    } finally {
        if ($WorkingDirectory -ne "") { Pop-Location }
        if ($null -ne $writer) { $writer.Dispose() }
        $ErrorActionPreference = $previous
    }
    if ($exitCode -ne 0) {
        throw "$What failed with exit code $exitCode"
    }
}

function Invoke-Compose {
    param([string[]]$Arguments, [string]$StdinText = $null, [string]$What = "docker compose")
    Invoke-Native -What $What -Executable "docker" -Arguments (@("compose", "-f", $script:ComposeFile) + $Arguments) -StdinText $StdinText
}

function Initialize-FairLines {
    if (-not ([System.Management.Automation.PSTypeName]"FairLines").Type) {
        Add-Type -TypeDefinition @"
using System.IO;
public static class FairLines {
    public static long Count(string path) {
        long n = 0; byte[] b = new byte[1 << 20];
        using (var f = File.OpenRead(path)) { int r; while ((r = f.Read(b, 0, b.Length)) > 0) for (int i = 0; i < r; i++) if (b[i] == 10) n++; }
        return n;
    }
}
"@
    }
}

# Set-Settings: validate the common parameters and derive dataset/output paths.
function Set-Settings {
    param($Count, $Warmup, [string]$BenchTime, $PerRegion, [string]$Dataset, [string]$OutputDirectory)
    $script:Count = [int]$Count
    $script:Warmup = [int]$Warmup
    $script:BenchTime = $BenchTime
    $script:PerRegion = [int]$PerRegion
    $script:RecordCount = $script:PerRegion * $script:Regions
    if ($script:PerRegion -lt 16384) {
        throw "-PerRegion must be at least 16384 (clustered-2 at q=16384 needs two full segments in R0)"
    }
    if ($script:PerRegion * ($script:Regions - 1) -lt 3 * $script:SegmentLeaves) {
        throw "subset too small for the scattered placement"
    }
    if ($Dataset -ne "") {
        # Resolve against PowerShell's current location, not the .NET process directory.
        $script:DatasetPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Dataset)
    } elseif ($script:PerRegion -eq $script:DefaultPerRegion) {
        $script:DatasetPath = $script:Existing10mFile
    } else {
        $script:DatasetPath = Join-Path $script:ExpDir ("datasets\spark_stratified_{0}.jsonl" -f $script:RecordCount)
    }
    if ($OutputDirectory -eq "") {
        $OutputDirectory = Join-Path $script:ExpDir ("results\spark-{0}" -f $script:RecordCount)
    }
    $script:OutputPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory)
    New-Item -ItemType Directory -Force -Path $script:OutputPath | Out-Null
}

# Confirm-Dataset: make sure the subset exists, has the expected record count
# and, for the existing 10M file, the expected SHA-256.
function Confirm-Dataset {
    if (-not (Test-Path -LiteralPath $script:DatasetPath)) {
        if (-not (Test-Path -LiteralPath $script:SourceDataset)) {
            throw "dataset $($script:DatasetPath) is missing and the source $($script:SourceDataset) is not available to regenerate it"
        }
        Write-Note "generating the deterministic subset ($($script:PerRegion) per region) -> $($script:DatasetPath)"
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $script:DatasetPath) | Out-Null
        # Write to a temporary name and rename: an interrupted run must not leave
        # a partial file that later runs mistake for the subset.
        $partial = "$($script:DatasetPath).partial"
        Remove-Item -LiteralPath $partial -Force -ErrorAction SilentlyContinue
        Invoke-Native -What "dataset selection" -Executable "go" -WorkingDirectory $script:RepoDir -Arguments @(
            "run", "dataset/scripts/select_fair_subset.go", "-input", $script:SourceDataset,
            "-output", $partial, "-regions", "$($script:Regions)", "-per-region", "$($script:PerRegion)")
        Move-Item -LiteralPath $partial -Destination $script:DatasetPath -Force
    }
    if ($env:FAIR_DATASET_FILE -eq $script:DatasetPath -and $env:FAIR_DATASET_SHA256) {
        $script:DatasetSha256 = $env:FAIR_DATASET_SHA256
    } else {
        Write-Note "hashing $($script:DatasetPath)"
        $script:DatasetSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $script:DatasetPath).Hash.ToLowerInvariant()
        Initialize-FairLines
        $lines = [FairLines]::Count($script:DatasetPath)
        if ($lines -ne $script:RecordCount) {
            throw "$($script:DatasetPath) has $lines records, expected $($script:RecordCount)"
        }
    }
    if ($script:DatasetPath -eq $script:Existing10mFile -and $script:DatasetSha256 -ne $script:Expected10mSha256) {
        throw "the existing 10M file has sha256 $($script:DatasetSha256), expected $($script:Expected10mSha256); it is not the dataset the earlier results used"
    }
    $env:FAIR_DATASET_FILE = $script:DatasetPath
    $env:FAIR_DATASET_SHA256 = $script:DatasetSha256
    Write-Note "dataset ok: $($script:RecordCount) records, sha256 $($script:DatasetSha256)"
}

# Start-Services: bring up the local PostgreSQL and Cassandra and create every
# schema and keyspace the four schemes need. Idempotent.
function Start-Services {
    if ($env:FAIR_SERVICES_READY -eq "1") { return }
    Write-Note "starting the local PostgreSQL and Cassandra containers"
    Invoke-Compose -What "starting the persistence services" -Arguments @("up", "-d", "--wait", "postgres", "cassandra")
    $psql = @("exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-q", "-U", "fair", "-d", "fair_benchmark")
    Invoke-Compose -What "PostgreSQL schema init" -Arguments ($psql + @("-f", "/docker-entrypoint-initdb.d/001-fair-benchmark.sql"))
    # The init files are copied into the containers and run with -f: text piped
    # from Windows PowerShell 5.1 arrives with a byte-order mark that cqlsh rejects.
    Invoke-Native -What "copy init_extra.sql" -Executable "docker" -Arguments @(
        "cp", (Join-Path $script:ExpDir "init_extra.sql"), "fair-postgres:/tmp/init_extra.sql")
    Invoke-Compose -What "PostgreSQL DHT/BLAS schema init" -Arguments ($psql + @("-f", "/tmp/init_extra.sql"))
    Invoke-Compose -What "HMF-Audit Cassandra init" -Arguments @("run", "--rm", "-T", "cassandra-init")
    Invoke-Compose -What "EVSEB Cassandra init" -Arguments @("run", "--rm", "-T", "evseb-cassandra-init")
    Invoke-Native -What "copy init_dht.cql" -Executable "docker" -Arguments @(
        "cp", (Join-Path $script:ExpDir "init_dht.cql"), "fair-cassandra:/tmp/init_dht.cql")
    Invoke-Compose -What "DHT-backed Cassandra init" -Arguments @("exec", "-T", "cassandra", "cqlsh", "-f", "/tmp/init_dht.cql")
    $env:FAIR_SERVICES_READY = "1"
}

$script:FairVariables = @(
    "CGO_ENABLED", "GOMAXPROCS", "FAIR_PERSISTENCE", "FAIR_DATASET_PATH", "FAIR_WARMUP", "FAIR_POSTGRES_DSN",
    "FAIR_DHT_POSTGRES_DSN", "FAIR_CASSANDRA_HOST", "FAIR_CASSANDRA_PORT", "FAIR_HMF_SHARDS_PER_REGION",
    "FAIR_HMF_SEGMENT_LEAVES", "FAIR_HMF_HPP_CONCURRENCY", "FAIR_REGION0_RECORDS", "FAIR_REGION1_RECORDS",
    "FAIR_EVSEB_DHT_CONCURRENCY", "FAIR_DHT_CONCURRENCY", "FAIR_DHT_PEERS", "FAIR_ABLATION",
    "FAIR_ABLATION_OUTPUT_DIR", "FAIR_ABLATION_WARMUP", "FAIR_ABLATION_MEASURED"
)

# Set-FairEnvironment: the variables the (unchanged) fair fixtures read.
function Set-FairEnvironment {
    $script:SavedEnvironment = @{}
    foreach ($name in $script:FairVariables) {
        $script:SavedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name)
    }
    $env:CGO_ENABLED = "0"
    $env:GOMAXPROCS = "1"
    $env:FAIR_PERSISTENCE = "1"
    $env:FAIR_DATASET_PATH = $script:DatasetPath
    $env:FAIR_WARMUP = [string]$script:Warmup
    $env:FAIR_POSTGRES_DSN = "postgres://fair:fairdev@127.0.0.1:55432/fair_benchmark?sslmode=disable"
    $env:FAIR_DHT_POSTGRES_DSN = "postgres://fair:fairdev@127.0.0.1:55432/fair_benchmark?sslmode=disable&search_path=dht"
    $env:FAIR_CASSANDRA_HOST = "127.0.0.1"
    $env:FAIR_CASSANDRA_PORT = "19042"
    $env:FAIR_HMF_SHARDS_PER_REGION = [string]$script:ShardsPerRegion
    $env:FAIR_HMF_SEGMENT_LEAVES = [string]$script:SegmentLeaves
    $env:FAIR_HMF_HPP_CONCURRENCY = [string]$script:HppConcurrency
    $env:FAIR_REGION0_RECORDS = [string]$script:PerRegion
    $env:FAIR_REGION1_RECORDS = [string]$script:PerRegion
    $env:FAIR_EVSEB_DHT_CONCURRENCY = [string]$script:EvsebDhtConcurrency
    $env:FAIR_DHT_CONCURRENCY = [string]$script:DhtConcurrency
    $env:FAIR_DHT_PEERS = [string]$script:DhtPeers
}

function Restore-FairEnvironment {
    if ($null -eq (Get-Variable -Name SavedEnvironment -Scope Script -ErrorAction SilentlyContinue)) { return }
    foreach ($name in $script:FairVariables) {
        [Environment]::SetEnvironmentVariable($name, $script:SavedEnvironment[$name])
    }
}

# Write-ExperimentEnvironment <experiment> <systems> <results dir>
function Write-ExperimentEnvironment {
    param([string]$Experiment, [string]$Systems, [string]$Results)
    Invoke-Native -What "environment record" -Executable $script:Python -Arguments @(
        (Join-Path $script:ExpDir "postprocess.py"), "env", "--out", $Results, "--experiment", $Experiment,
        "--systems", $Systems, "--dataset", $script:DatasetPath, "--dataset-sha256", $script:DatasetSha256,
        "--source-dataset", $script:SourceDataset, "--per-region", "$($script:PerRegion)",
        "--regions", "$($script:Regions)", "--count", "$($script:Count)", "--warmup", "$($script:Warmup)",
        "--bench-time", $script:BenchTime, "--shards-per-region", "$($script:ShardsPerRegion)",
        "--segment-leaves", "$($script:SegmentLeaves)")
}

# Invoke-FairBenchmark <module dir> <package> <benchmark regexp> <result file>
function Invoke-FairBenchmark {
    param([string]$Directory, [string]$Package, [string]$Pattern, [string]$ResultFile)
    Write-Note "benchmark $Pattern ($Directory): count $($script:Count), window $($script:BenchTime), warm-up $($script:Warmup), GOMAXPROCS=1"
    Invoke-Native -What "$Directory benchmark $Pattern" -Executable "go" -LogFile $ResultFile `
        -WorkingDirectory (Join-Path $script:SystemsDir $Directory) -Arguments @(
        "test", "-run", "^$", "-bench", $Pattern, "-benchmem", "-count", "$($script:Count)",
        "-benchtime", $script:BenchTime, "-cpu", "1", "-timeout", "0", $Package)
}

# Start-Experiment: common start-up of every experiment script. Returns the
# experiment's results directory.
function Start-Experiment {
    param([string]$Name, $Count, $Warmup, [string]$BenchTime, $PerRegion, [string]$Dataset, [string]$OutputDirectory)
    Set-Settings -Count $Count -Warmup $Warmup -BenchTime $BenchTime -PerRegion $PerRegion -Dataset $Dataset -OutputDirectory $OutputDirectory
    $script:Python = Find-Python
    foreach ($tool in @("go", "docker")) {
        if ($null -eq (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool is not installed" }
    }
    Confirm-Dataset
    Start-Services
    Set-FairEnvironment
    $results = Join-Path $script:OutputPath $Name
    New-Item -ItemType Directory -Force -Path $results | Out-Null
    Write-Note "experiment $Name -> $results"
    return $results
}
