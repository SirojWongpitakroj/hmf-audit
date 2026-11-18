#!/usr/bin/env bash
# Resource-guarded parallel runner for ingest.sh --parallel. Sourced, not run.
#
# Running the four schemes' ingests at once is only worth it while the runner
# keeps up. This scheduler therefore
#   * never runs more jobs than CPUs (INGEST_MAX_PARALLEL can only lower that),
#   * gives each job an equal share of the CPUs for its setup threads
#     (FAIR_SETUP_GOMAXPROCS) and lowers its priority so SSH stays responsive,
#   * starts a job only if the memory budget (total RAM minus a reserve) still
#     covers the estimated peak of every running job plus the new one,
#   * watches available memory and, if it falls below the floor, stops the
#     newest running job (never the last one) and runs it again alone once
#     the others are done. Ingest is idempotent: a job truncates and reloads
#     its own tables, so a stopped job loses only time.
# A job that fails for any other reason is not retried and does not stop the
# others; the caller is told which ones failed.
#
# Caller provides: note(), ingest_one <system>, and PAR_LOG_DIR (per-job logs).
# Settings (all optional environment variables):
#   INGEST_MAX_PARALLEL      most jobs at once (default: number of jobs, at most CPUs)
#   INGEST_MEM_RESERVE_MB    RAM kept free (default: the larger of 1024 and 10% of RAM)
#   INGEST_MEM_MB_<SYSTEM>   estimated peak of a job, e.g. INGEST_MEM_MB_HMF_AUDIT=9000
#   INGEST_POLL_SECONDS      how often the scheduler looks (default 3)

# Estimated peak memory in MB at 10M records. Not measured at 10M: extrapolated
# from the peak working set of a 100k ingest (HMF-Audit 134 MB, EVSEB 81 MB,
# DHT-backed 84 MB, BLAS 67 MB) and rounded up, so they err on the safe side.
declare -gA PAR_DEFAULT_MB=([hmf-audit]=8000 [evseb]=3000 [dht-backed]=3000 [blas]=1500)

PAR_MEMINFO_FILE="${PAR_MEMINFO_FILE:-/proc/meminfo}"

par_cpus() {
    local n
    n="$(nproc 2>/dev/null)" || n="${NUMBER_OF_PROCESSORS:-1}"
    echo "${n:-1}"
}

# par_meminfo <field>: the kB value of a /proc/meminfo field, empty if absent.
par_meminfo() { awk -v f="$1:" '$1 == f {print $2; exit}' "$PAR_MEMINFO_FILE" 2>/dev/null; return 0; }

par_mem_total_mb() {
    local kb
    kb="$(par_meminfo MemTotal)"
    [[ -z "$kb" ]] || echo $((kb / 1024))
    return 0
}

# Empty when the platform cannot say how much memory is available (Git Bash on
# Windows reports only MemFree, which ignores the file cache and would look
# like constant pressure), in which case the watchdog stays off.
par_mem_avail_mb() {
    local kb
    kb="$(par_meminfo MemAvailable)"
    [[ -z "$kb" ]] || echo $((kb / 1024))
    return 0
}

# par_need_mb <system> <records>
par_need_mb() {
    local var="INGEST_MEM_MB_${1//-/_}" mb
    var="${var^^}"
    mb="${!var:-}"
    if [[ -z "$mb" ]]; then
        mb=$((${PAR_DEFAULT_MB[$1]:-4000} * $2 / 10000000))
        ((mb >= 256)) || mb=256
    fi
    echo "$mb"
}

# par_kill_tree <pid>: stop a job and everything it started.
par_kill_tree() {
    local pid="$1"
    if [[ -r "/proc/$pid/winpid" ]] && command -v taskkill >/dev/null 2>&1; then
        taskkill //F //T //PID "$(cat "/proc/$pid/winpid")" >/dev/null 2>&1 || true
    else
        kill -TERM -- "-$pid" 2>/dev/null || true # the job's own process group (set -m)
        if command -v pkill >/dev/null 2>&1; then pkill -TERM -P "$pid" 2>/dev/null || true; fi
    fi
    kill -TERM "$pid" 2>/dev/null || true
}

PAR_RUNNING=() PAR_QUEUE=() PAR_DEFERRED=() PAR_FAILED=()
declare -gA PAR_PID=() PAR_START=() PAR_NEED=() PAR_STOPPED=()
PAR_MAX_OVERLAP=0

par_launch() {
    local name="$1" need="$2"
    note "starting $name (estimated peak ${need} MB; ${#PAR_RUNNING[@]} already running); log $PAR_LOG_DIR/$name.log"
    (
        if command -v renice >/dev/null 2>&1; then renice -n 10 -p "$BASHPID" >/dev/null 2>&1 || true; fi
        ingest_one "$name"
    ) >"$PAR_LOG_DIR/$name.log" 2>&1 &
    PAR_PID[$name]=$!
    PAR_START[$name]=$SECONDS
    PAR_NEED[$name]=$need
    PAR_RUNNING+=("$name")
    ((${#PAR_RUNNING[@]} <= PAR_MAX_OVERLAP)) || PAR_MAX_OVERLAP=${#PAR_RUNNING[@]}
}

par_abort() {
    local name
    note "interrupted: stopping every running ingest"
    for name in ${PAR_RUNNING[@]+"${PAR_RUNNING[@]}"}; do par_kill_tree "${PAR_PID[$name]}"; done
    wait 2>/dev/null || true
    exit 130
}

# par_reap: collect finished jobs.
par_reap() {
    local name pid status secs
    local -a still=()
    for name in ${PAR_RUNNING[@]+"${PAR_RUNNING[@]}"}; do
        pid="${PAR_PID[$name]}"
        if kill -0 "$pid" 2>/dev/null; then still+=("$name"); continue; fi
        status=0
        wait "$pid" || status=$?
        secs=$((SECONDS - PAR_START[$name]))
        if [[ -n "${PAR_STOPPED[$name]:-}" ]]; then
            unset "PAR_STOPPED[$name]"
            note "$name was stopped under memory pressure after ${secs}s; it will run again alone"
            PAR_DEFERRED+=("$name")
        elif ((status == 0)); then
            note "$name finished in ${secs}s"
        else
            note "$name FAILED (exit $status) after ${secs}s; the end of $PAR_LOG_DIR/$name.log:"
            tail -n 15 "$PAR_LOG_DIR/$name.log" >&2 || true
            PAR_FAILED+=("$name")
        fi
    done
    PAR_RUNNING=(${still[@]+"${still[@]}"})
}

# parallel_ingest <records> <system>...
# Returns 0 when every job succeeded; the failed ones are left in PAR_FAILED.
parallel_ingest() {
    local records="$1"
    shift
    PAR_QUEUE=("$@") PAR_RUNNING=() PAR_DEFERRED=() PAR_FAILED=() PAR_MAX_OVERLAP=0
    PAR_STOPPED=() PAR_PID=() PAR_START=() PAR_NEED=()

    local cpus cap total reserve budget floor poll avail used name need next
    cpus="$(par_cpus)"
    cap="${INGEST_MAX_PARALLEL:-${#PAR_QUEUE[@]}}"
    [[ "$cap" =~ ^[1-9][0-9]*$ ]] || { note "INGEST_MAX_PARALLEL must be a positive integer"; return 1; }
    ((cap <= cpus)) || cap=$cpus
    poll="${INGEST_POLL_SECONDS:-3}"
    # Setup threads per job: an equal share of the CPUs, so jobs together do not oversubscribe.
    [[ -n "${FAIR_SETUP_GOMAXPROCS:-}" ]] || export FAIR_SETUP_GOMAXPROCS=$(((cpus + cap - 1) / cap))

    total="$(par_mem_total_mb)"
    avail="$(par_mem_avail_mb)"
    budget="" floor=0 reserve=0
    local memory_note="memory budget off (total memory cannot be read)"
    if [[ -n "$total" ]]; then
        if [[ -n "${INGEST_MEM_RESERVE_MB:-}" ]]; then
            reserve="$INGEST_MEM_RESERVE_MB"
        else
            reserve=$((total / 10))
            ((reserve >= 1024)) || reserve=1024
        fi
        budget=$((total - reserve))
        floor=$((reserve / 2))
        ((floor >= 256)) || floor=256
        memory_note="memory: ${total} MB total, ${reserve} MB reserve, ${budget} MB budget"
        if [[ -n "$avail" ]]; then
            memory_note+=", watchdog floor ${floor} MB"
        else
            memory_note+=", pressure watchdog off (available memory cannot be read)"
        fi
    fi
    note "parallel ingest: $cap at a time on $cpus CPUs, ${FAIR_SETUP_GOMAXPROCS} setup thread(s) each; $memory_note"

    set -m # one process group per job, so a whole job can be stopped
    trap par_abort INT TERM

    while ((${#PAR_QUEUE[@]} > 0 || ${#PAR_RUNNING[@]} > 0)); do
        par_reap

        # Pressure watchdog: stop the newest job, but never the only one.
        avail="$(par_mem_avail_mb)"
        if [[ -n "$avail" ]] && ((avail < floor)) && ((${#PAR_RUNNING[@]} > 1)) && ((${#PAR_STOPPED[@]} == 0)); then
            name="${PAR_RUNNING[${#PAR_RUNNING[@]} - 1]}"
            note "only ${avail} MB of memory available (floor ${floor} MB): stopping $name"
            PAR_STOPPED[$name]=1
            par_kill_tree "${PAR_PID[$name]}"
        fi

        # Admission: the next job starts only if it fits.
        while ((${#PAR_QUEUE[@]} > 0 && ${#PAR_RUNNING[@]} < cap)); do
            next="${PAR_QUEUE[0]}"
            need="$(par_need_mb "$next" "$records")"
            if ((${#PAR_RUNNING[@]} > 0)); then
                ((${#PAR_STOPPED[@]} == 0)) || break
                if [[ -n "$budget" ]]; then
                    used=0
                    for name in "${PAR_RUNNING[@]}"; do used=$((used + PAR_NEED[$name])); done
                    ((used + need <= budget)) || break
                fi
                avail="$(par_mem_avail_mb)"
                [[ -z "$avail" ]] || ((avail >= reserve)) || break
            elif [[ -n "$budget" ]] && ((need > budget)); then
                note "warning: $next alone is estimated at ${need} MB, above the ${budget} MB budget; running it alone anyway"
            fi
            PAR_QUEUE=("${PAR_QUEUE[@]:1}")
            par_launch "$next" "$need"
        done

        ((${#PAR_QUEUE[@]} > 0 || ${#PAR_RUNNING[@]} > 0)) && sleep "$poll"
    done

    # Jobs stopped for memory run again one at a time, with the machine to themselves.
    for name in ${PAR_DEFERRED[@]+"${PAR_DEFERRED[@]}"}; do
        note "running $name alone"
        if ! ( ingest_one "$name" ) >"$PAR_LOG_DIR/$name.log" 2>&1; then
            note "$name FAILED when run alone; the end of $PAR_LOG_DIR/$name.log:"
            tail -n 15 "$PAR_LOG_DIR/$name.log" >&2 || true
            PAR_FAILED+=("$name")
        fi
    done

    trap - INT TERM
    set +m
    ((${#PAR_FAILED[@]} == 0))
}
