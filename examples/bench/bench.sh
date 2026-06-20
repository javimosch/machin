#!/usr/bin/env sh
# Reproducible fib(40) benchmark: MFL vs hand-written C vs Rust.
#
# Reproduces the README "Performance" table. Each implementation is built
# with the same optimization level the README claims (-O2 / -O), then run
# REPS times; the best wall-clock time is reported (best-of-N reduces noise
# from scheduler jitter). Rust is optional — skipped with a note if rustc
# is absent, so the report never silently drops a row without saying so.
#
# Usage:
#   examples/bench/bench.sh            # uses ./bin/machin, 5 reps
#   MACHIN=path REPS=10 examples/bench/bench.sh
set -eu

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MACHIN=${MACHIN:-./bin/machin}
CC=${CC:-cc}
REPS=${REPS:-5}
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

if [ ! -x "$MACHIN" ]; then
    echo "error: machin binary not found at '$MACHIN'" >&2
    echo "       run 'make build' first, or set MACHIN=/path/to/machin" >&2
    exit 1
fi

# best_time CMD... -> echoes the smallest real-time (seconds) over $REPS runs.
best_time() {
    best=""
    i=0
    while [ "$i" -lt "$REPS" ]; do
        i=$((i + 1))
        # %e is real (wall-clock) seconds; portable across GNU/BSD time.
        t=$( { /usr/bin/env time -f '%e' "$@" >/dev/null; } 2>&1 ) || \
            t=$( { TIMEFORMAT='%R'; time "$@" >/dev/null; } 2>&1 | tr -d '\n' )
        case "$t" in *[!0-9.]*|'') continue;; esac
        if [ -z "$best" ] || awk "BEGIN{exit !($t < $best)}"; then best=$t; fi
    done
    echo "${best:-n/a}"
}

echo "fib(40), best of $REPS runs"
echo

# --- MFL (native, via machin -> cc -O2) ---
"$MACHIN" build "$DIR/fib.mfl" -o "$OUT/fib_mfl"
mfl=$(best_time "$OUT/fib_mfl")

# --- hand-written C (cc -O2) ---
if command -v "$CC" >/dev/null 2>&1; then
    "$CC" -O2 "$DIR/fib.c" -o "$OUT/fib_c"
    c=$(best_time "$OUT/fib_c")
else
    c="n/a (no $CC)"
fi

# --- Rust (rustc -O), optional ---
if command -v rustc >/dev/null 2>&1; then
    rustc -O "$DIR/fib.rs" -o "$OUT/fib_rs" 2>/dev/null
    rust=$(best_time "$OUT/fib_rs")
else
    rust="n/a (rustc not installed)"
fi

# Append "s" only to bare numeric times, never to "n/a ..." messages.
secs() { case "$1" in ''|*[!0-9.]*) echo "$1";; *) echo "${1}s";; esac; }

printf '| %-26s | %-22s |\n' "Implementation" "Time"
printf '| %-26s | %-22s |\n' "--------------------------" "----------------------"
printf '| %-26s | %-22s |\n' "MFL (native, cc -O2)"      "$(secs "$mfl")"
printf '| %-26s | %-22s |\n' "hand-written C (cc -O2)"   "$(secs "$c")"
printf '| %-26s | %-22s |\n' "Rust (rustc -O)"           "$(secs "$rust")"
echo
echo "Note: absolute times are machine-dependent; the ratios are the point."
