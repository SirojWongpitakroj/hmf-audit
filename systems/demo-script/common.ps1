# Shared by verification-query.ps1 and tamper.ps1 so both read the attribute
# query identically. Dot-source this file; do not run it. Local Docker only.
. (Join-Path $PSScriptRoot "..\attribute-query\lib.ps1")
$script:ExpectedRecords = 100000
# The log generator's copy of the 100k logs: where the original content of a tampered log is read from.
$script:Source100k = Join-Path $script:SystemsDir "experiments\datasets\spark_stratified_100000.jsonl"

# Stop-Demo: report an error and exit with code 2 (1 is reserved for REJECTED).
function Stop-Demo {
    param([string]$Message)
    Write-Error $Message -ErrorAction Continue
    exit 2
}

# Invoke-Capture: run a program, capture stdout and stderr separately, return
# @{ ExitCode; Stdout; Stderr }. Used where the exit code and both streams matter.
function Invoke-Capture {
    param([string]$Executable, [string[]]$Arguments = @())
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $Executable
    $info.Arguments = ($Arguments | ForEach-Object { '"' + ($_ -replace '"', '\"') + '"' }) -join " "
    $info.UseShellExecute = $false
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $process = [System.Diagnostics.Process]::Start($info)
    $stderrTask = $process.StandardError.ReadToEndAsync()
    $stdout = $process.StandardOutput.ReadToEnd()
    $process.WaitForExit()
    return @{ ExitCode = $process.ExitCode; Stdout = $stdout; Stderr = $stderrTask.Result }
}

# Invoke-Provider: run SQL against the storage provider's database (the local
# fair-postgres container). Returns the output lines.
function Invoke-Provider {
    param([string]$Sql)
    $result = Invoke-Capture -Executable "docker" -Arguments @("exec", "fair-postgres", "psql", "-U", "fair",
        "-d", "fair_benchmark", "-q", "-t", "-A", "-v", "ON_ERROR_STOP=1", "-c", $Sql)
    if ($result.ExitCode -ne 0) { throw "provider query failed: $($result.Stderr.Trim())" }
    return @($result.Stdout -split "`r?`n" | Where-Object { $_ -ne "" })
}

# Assert-Demo100k: both scripts work on the 100k ingest only. Returns the Python path.
function Assert-Demo100k {
    foreach ($tool in @("go", "docker")) {
        if ($null -eq (Get-Command $tool -ErrorAction SilentlyContinue)) { Stop-Demo "$tool is not installed" }
    }
    $python = Find-Python
    try { $rows = (Invoke-Provider "SELECT count(*) FROM hmfq.encrypted_logs") -join "" } catch { $rows = "" }
    if ($rows -ne "$script:ExpectedRecords") {
        Stop-Demo "needs the 100k ingest (found '$rows' rows in hmfq.encrypted_logs); run: systems\attribute-query\ingest.ps1 -Records 100000"
    }
    return $python
}

# Invoke-AuditQuery: run the auditor for the attribute query. Returns
# @{ ExitCode; Stdout; Stderr; Description }.
function Invoke-AuditQuery {
    param([string]$Tenant, [string]$Service, [string]$LogType, [string]$Region,
          [string]$At, [string]$Start, [string]$End, [switch]$WithKey)
    if ($At -eq "" -and ($Start -eq "" -or $End -eq "")) { Stop-Demo "give a time: -At T, or both -Start and -End" }
    $arguments = @("-json", "-tenant", $Tenant, "-service", $Service, "-log-type", $LogType, "-region", $Region)
    $description = "tenant=$Tenant service=$Service log_type=$LogType region=$Region"
    if ($At -ne "") { $arguments += @("-at", $At); $description += " time=$At" }
    else { $arguments += @("-start", $Start, "-end", $End); $description += " time=[$Start, $End)" }
    # With -WithKey the auditor also reports the stored content of rejected logs.
    if ($WithKey -and (Test-Path -LiteralPath $script:KeyFile)) { $arguments += @("-key-file", $script:KeyFile) }
    $result = Invoke-Capture -Executable (Build-Auditor) -Arguments $arguments
    $result.Description = $description
    return $result
}
