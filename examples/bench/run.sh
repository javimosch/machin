#!/usr/bin/env bash
#
# Reproducible fib(40) benchmark for MFL vs hand-written C vs Rust.
#
# Builds whichever toolchains are available, runs each binary several
# times, keeps the best (lowest) wall-clock time, verifies every
# implementation prints the same result, and emits a Markdown table.
#
# Usage:
#   examples/bench/run.sh            # human-readable report to stdout
#   examples/bench/run.sh --md       # Markdown table only (for docs)
#   RUNS=10 examples/bench/run.sh     # override repetition count
#
# Exit status is non-zero if any implementation disagrees on the result.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

RUNS="${RUNS:-5}"
EXPECTED="102334155"   # fib(40)
MD_ONLY=0
[ "${1:-}" = "--md" ] && MD_ONLY=1

log() { [ "$MD_ONLY" = "1" ] || echo "$@" >&2; }

# best-of-N wall time in seconds (3 decimals); echoes the seconds.
time_best() {
    local bin="$1" best="" t
    local i
    for ((i = 0; i < RUNS; i++)); do
        # %e is elapsed real seconds; portable across GNU/BSD `time` builtins
        t="$( { TIMEFORMAT='%R'; time "$bin" >/dev/null; } 2>&1 )"
        if [ -z "$best" ] || awk "BEGIN{exit !($t < $best)}"; then best="$t"; fi
    done
    echo "$best"
}

check() {
    local bin="$1" got
    got="$("$bin")"
    [ "$got" = "$EXPECTED" ] || {
        echo "FAIL: $bin printed '$got', expected '$EXPECTED'" >&2
        exit 1
    }
}

declare -a NAMES TIMES NOTES

# --- MFL -------------------------------------------------------------------
log "building MFL toolchain..."
( cd "$ROOT" && go build -o "$OUT/machin" . )
log "compiling fib.mfl -> native..."
"$OUT/machin" build "$HERE/fib.mfl" -o "$OUT/fib_mfl"
check "$OUT/fib_mfl"
NAMES+=("MFL (native, cc -O2)"); TIMES+=("$(time_best "$OUT/fib_mfl")")
NOTES+=("emits C, optimized by the system compiler")

# --- hand-written C --------------------------------------------------------
if command -v cc >/dev/null 2>&1; then
    log "compiling fib.c -> native..."
    cc -O2 -o "$OUT/fib_c" "$HERE/fib.c"
    check "$OUT/fib_c"
    NAMES+=("hand-written C (cc -O2)"); TIMES+=("$(time_best "$OUT/fib_c")")
    NOTES+=("the baseline MFL compiles to")
else
    log "cc not found — skipping C baseline"
fi

# --- Rust ------------------------------------------------------------------
if command -v rustc >/dev/null 2>&1; then
    log "compiling fib.rs -> native..."
    rustc -O -o "$OUT/fib_rs" "$HERE/fib.rs" 2>/dev/null
    check "$OUT/fib_rs"
    NAMES+=("Rust (rustc -O)"); TIMES+=("$(time_best "$OUT/fib_rs")")
    NOTES+=("for reference")
else
    log "rustc not found — skipping Rust comparison"
fi

# --- report ----------------------------------------------------------------
[ "$MD_ONLY" = "1" ] || {
    echo >&2
    echo "fib(40), best of $RUNS runs — host: $(uname -s) $(uname -m)" >&2
}
echo "| Implementation | Time | Notes |"
echo "|----------------|------|-------|"
for i in "${!NAMES[@]}"; do
    printf "| %s | %ss | %s |\n" "${NAMES[$i]}" "${TIMES[$i]}" "${NOTES[$i]}"
done
