# Shared helpers for ingest.ps1 and query.ps1. Dot-source this file; do not run it.
# Everything targets the local Docker stack in systems/fair-persistence
# (PostgreSQL 127.0.0.1:55432, Cassandra 127.0.0.1:19042).
. (Join-Path $PSScriptRoot "..\experiments\lib.ps1")
$script:AqDir = $PSScriptRoot
$script:KeyFile = Join-Path $script:AqDir "keys\hmf-ingest.key"
$script:Keyspace = "hmf_audit_q"

# Start-AttributeServices: start PostgreSQL and Cassandra and create this
# tool's own schema (hmfq) and keyspace (hmf_audit_q). Idempotent.
function Start-AttributeServices {
    Write-Note "starting the local PostgreSQL and Cassandra containers"
    Invoke-Compose -What "starting the persistence services" -Arguments @("up", "-d", "--wait", "postgres", "cassandra")
    # Init files are copied into the containers and run with -f: text piped from
    # Windows PowerShell 5.1 carries a byte-order mark that cqlsh rejects.
    Invoke-Native -What "copy init_pg.sql" -Executable "docker" -Arguments @(
        "cp", (Join-Path $script:AqDir "init_pg.sql"), "fair-postgres:/tmp/init_pg_q.sql")
    Invoke-Compose -What "PostgreSQL schema init" -Arguments @("exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-q",
        "-U", "fair", "-d", "fair_benchmark", "-f", "/tmp/init_pg_q.sql")
    $source = Join-Path $script:SystemsDir "fair-persistence\docker\cassandra\init-hmf.cql"
    $cql = (Get-Content -Raw -LiteralPath $source) -replace "hmf_audit", $script:Keyspace
    $temp = Join-Path $script:AqDir ".init_cassandra.cql"
    [System.IO.File]::WriteAllText($temp, $cql, (New-Object System.Text.UTF8Encoding($false)))
    try {
        Invoke-Native -What "copy Cassandra schema" -Executable "docker" -Arguments @("cp", $temp, "fair-cassandra:/tmp/init_hmf_q.cql")
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
    Invoke-Compose -What "HMF-Audit keyspace init" -Arguments @("exec", "-T", "cassandra", "cqlsh", "-f", "/tmp/init_hmf_q.cql")
}

# Build-Auditor: compile cmd/hmf-auditor into attribute-query\bin and return its
# path. `go run` would turn the auditor's exit codes (0 VALID, 1 REJECTED,
# 2 error) into 1, so callers run the binary directly.
function Build-Auditor {
    $bin = Join-Path $script:AqDir "bin\hmf-auditor.exe"
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $bin) | Out-Null
    Invoke-Native -What "build hmf-auditor" -Executable "go" -WorkingDirectory (Join-Path $script:SystemsDir "hmf-audit") `
        -Arguments @("build", "-o", $bin, "./cmd/hmf-auditor")
    return $bin
}
