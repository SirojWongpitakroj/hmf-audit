#!/usr/bin/env bash
# Tests for parallel.sh with fake jobs (no databases). Run: bash scripts/test_parallel.sh
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/parallel.sh"

note() { :; }
export INGEST_POLL_SECONDS=0.1
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
failures=0
check() { # <description> <condition result: 0 = ok>
    if [[ "$2" == 0 ]]; then echo "ok   $1"; else echo "FAIL $1"; failures=$((failures + 1)); fi
}
fresh() { rm -rf "$TMP/run"; mkdir -p "$TMP/run"; PAR_LOG_DIR="$TMP/run"; : >"$TMP/run/marks"; }

# --- parsing ---------------------------------------------------------------
cat >"$TMP/meminfo_linux" <<'EOF'
MemTotal:       16384000 kB
MemFree:          500000 kB
MemAvailable:    9000000 kB
EOF
cat >"$TMP/meminfo_msys" <<'EOF'
MemTotal:       16474168 kB
MemFree:          762432 kB
EOF
PAR_MEMINFO_FILE="$TMP/meminfo_linux"
[[ "$(par_mem_total_mb)" == 16000 && "$(par_mem_avail_mb)" == 8789 ]]
check "reads MemTotal and MemAvailable" $?
PAR_MEMINFO_FILE="$TMP/meminfo_msys"
[[ "$(par_mem_total_mb)" == 16088 && -z "$(par_mem_avail_mb)" ]]
check "no MemAvailable means the watchdog stays off" $?

[[ "$(par_need_mb hmf-audit 10000000)" == 8000 && "$(par_need_mb hmf-audit 100000)" == 256 ]]
check "memory estimate scales with records and has a floor" $?
INGEST_MEM_MB_DHT_BACKED=1234 bash -c '. "'"$HERE"'/parallel.sh"; [[ "$(par_need_mb dht-backed 100000)" == 1234 ]]'
check "memory estimate can be overridden" $?

# --- scheduling ------------------------------------------------------------
# Fake job: records start and end, sleeps, optionally fails.
ingest_one() {
    echo "start $1 $(date +%s.%N)" >>"$PAR_LOG_DIR/marks"
    sleep "${FAKE_SLEEP:-0.6}"
    if [[ "$1" == "${FAKE_FAIL:-}" ]]; then echo "boom" >&2; return 3; fi
    echo "end $1 $(date +%s.%N)" >>"$PAR_LOG_DIR/marks"
}
overlap() { # highest number of jobs between a start and its end
    awk '$1=="start"{n++; if(n>m)m=n} $1=="end"{n--} END{print m+0}' "$PAR_LOG_DIR/marks"
}

PAR_MEMINFO_FILE="$TMP/meminfo_msys"
export FAIR_SETUP_GOMAXPROCS=1 # keep the fake runs from exporting their own

fresh
INGEST_MAX_PARALLEL=2 parallel_ingest 100000 a b c d e
rc=$?
[[ $rc == 0 && "$(overlap)" -le 2 && "$(grep -c '^end' "$PAR_LOG_DIR/marks")" == 5 ]]
result=$?
check "never more than INGEST_MAX_PARALLEL jobs, all complete (overlap $(overlap))" "$result"

fresh
INGEST_MAX_PARALLEL=1000 parallel_ingest 100000 a b c d e f g h i j k l m n o p q r s t >/dev/null 2>&1
cpus="$(par_cpus)"
[[ "$PAR_MAX_OVERLAP" -le "$cpus" ]]
check "never more jobs than CPUs ($PAR_MAX_OVERLAP at once, $cpus CPUs)" $?

fresh
INGEST_MAX_PARALLEL=4 INGEST_MEM_RESERVE_MB=1000 INGEST_MEM_MB_A=9000 INGEST_MEM_MB_B=9000 INGEST_MEM_MB_C=9000 \
    parallel_ingest 100000 a b c
[[ "$(overlap)" == 1 ]]
result=$?
check "memory budget (16088 - 1000 MB) lets only one 9000 MB job run at a time (overlap $(overlap))" "$result"

fresh
INGEST_MAX_PARALLEL=4 INGEST_MEM_RESERVE_MB=1000 INGEST_MEM_MB_A=5000 INGEST_MEM_MB_B=5000 INGEST_MEM_MB_C=5000 \
    INGEST_MEM_MB_D=1000 bash -c '
    . "'"$HERE"'/parallel.sh"; note() { :; }; PAR_MEMINFO_FILE="'"$TMP"'/meminfo_msys"; PAR_LOG_DIR="'"$PAR_LOG_DIR"'"
    ingest_one() { sleep 0.5; }
    parallel_ingest 100000 a b d && echo "$PAR_MAX_OVERLAP" >"'"$PAR_LOG_DIR"'/overlap"'
[[ "$(cat "$PAR_LOG_DIR/overlap" 2>/dev/null)" == 3 ]]
check "jobs that fit in the budget do run together (5000+5000+1000 <= 15088)" $?

fresh
FAKE_FAIL=c parallel_ingest 100000 a b c d >/dev/null 2>&1
rc=$?
[[ $rc != 0 && "${PAR_FAILED[*]}" == "c" && "$(grep -c '^end' "$PAR_LOG_DIR/marks")" == 3 ]]
check "a failing job fails the run without stopping the others (failed: ${PAR_FAILED[*]})" $?

# --- memory-pressure watchdog ---------------------------------------------
# While a pressure file exists, available memory reads as 100 MB. Job b creates
# it the first time it runs and removes it when it runs again.
par_mem_avail_mb() { if [[ -e "$TMP/pressure" ]]; then echo 100; else echo 9000; fi; }
par_mem_total_mb() { echo 16000; }
ingest_one() {
    echo "start $1" >>"$PAR_LOG_DIR/marks"
    if [[ "$1" == b ]]; then
        if [[ ! -e "$TMP/b.ran" ]]; then
            touch "$TMP/b.ran" "$TMP/pressure"
            sleep 30 # long enough that only the watchdog can end it
        else
            rm -f "$TMP/pressure"
        fi
    else
        sleep 1.5
    fi
    echo "end $1" >>"$PAR_LOG_DIR/marks"
}
fresh
rm -f "$TMP/pressure" "$TMP/b.ran"
INGEST_MAX_PARALLEL=2 INGEST_MEM_RESERVE_MB=1000 parallel_ingest 100000 a b
rc=$?
[[ $rc == 0 ]]
check "run succeeds after a job was stopped for memory" $?
[[ "$(grep -c '^start b' "$PAR_LOG_DIR/marks")" == 2 && "$(grep -c '^end b' "$PAR_LOG_DIR/marks")" == 1 ]]
check "the stopped job ran twice (stopped once, finished the second time alone)" $?
[[ "$(grep -c '^end a' "$PAR_LOG_DIR/marks")" == 1 ]]
check "the other job was left alone and finished" $?

# The only running job is never stopped, however low memory is.
par_mem_avail_mb() { echo 50; }
fresh
ingest_one() { echo "start $1" >>"$PAR_LOG_DIR/marks"; sleep 0.5; echo "end $1" >>"$PAR_LOG_DIR/marks"; }
INGEST_MAX_PARALLEL=2 INGEST_MEM_RESERVE_MB=1000 parallel_ingest 100000 solo
[[ $? == 0 && "$(grep -c '^end solo' "$PAR_LOG_DIR/marks")" == 1 ]]
check "a lone job is never stopped, even with no memory available" $?

echo
if ((failures == 0)); then echo "all parallel.sh tests passed"; else echo "$failures test(s) failed"; exit 1; fi
