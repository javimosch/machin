#!/usr/bin/env bash
# Reproducible fib(40) benchmark: MFL (native) vs hand-written C vs Rust.
#
# Produces the table in the README "Performance" section on YOUR machine, so the
# numbers are verifiable rather than asserted. Absolute times depend on hardware;
# what matters is that MFL lands in the same class as the C it compiles to.
#
# Usage:  examples/bench/run_bench.sh [runs]
#   runs  number of timed repetitions per implementation (default 5; best is kept)
#
# Requires: go, a C compiler (cc/gcc/clang). Rust (rustc) is optional and skipped
# if absent. Run from the repository root or from examples/bench/.
set -euo pipefail

RUNS="${1:-5}"

# Resolve repo root relative to this script so it works from any CWD.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

CC="${CC:-cc}"

echo "machin fib(40) benchmark — best of $RUNS runs"
echo "host: $(uname -s) $(uname -m)"
echo

# --- Build the machin toolchain --------------------------------------------
echo "building machin toolchain..."
( cd "$ROOT" && go build -trimpath -o "$WORK/machin" . )

# --- Compile each implementation to a native binary -------------------------
echo "compiling fib (MFL / C / Rust)..."
"$WORK/machin" build "$SCRIPT_DIR/fib.mfl" -o "$WORK/fib_mfl" >/dev/null
"$CC" -O2 "$SCRIPT_DIR/fib.c" -o "$WORK/fib_c"

HAVE_RUST=0
if command -v rustc >/dev/null 2>&1; then
    rustc -O "$SCRIPT_DIR/fib.rs" -o "$WORK/fib_rs"
    HAVE_RUST=1
fi
echo

# --- Time a binary: best wall-clock over $RUNS executions -------------------
# Prints seconds with millisecond resolution.
best_time() {
    local bin="$1" best="" t
    local TIMEFORMAT='%R'   # bash `time` keyword honours this shell variable
    for _ in $(seq "$RUNS"); do
        # `time` writes only the real-time seconds (%R) to stderr.
        t="$( { time "$bin" >/dev/null; } 2>&1 )"
        if [ -z "$best" ] || awk "BEGIN{exit !($t < $best)}"; then best="$t"; fi
    done
    printf '%s' "$best"
}

# Verify all implementations agree on the answer before trusting the timings.
EXPECT="$("$WORK/fib_mfl")"
[ "$("$WORK/fib_c")" = "$EXPECT" ] || { echo "C output mismatch!" >&2; exit 1; }
if [ "$HAVE_RUST" = 1 ]; then
    [ "$("$WORK/fib_rs")" = "$EXPECT" ] || { echo "Rust output mismatch!" >&2; exit 1; }
fi

MFL_T="$(best_time "$WORK/fib_mfl")"
C_T="$(best_time "$WORK/fib_c")"

printf '| %-26s | %-6s | %s\n' "Implementation" "Time" "Notes"
printf '|%s|%s|%s\n' "----------------------------" "--------" "-------"
printf '| %-26s | %5ss | emits C, optimized by cc -O2\n' "MFL (native, cc -O2)" "$MFL_T"
printf '| %-26s | %5ss | the baseline MFL compiles to\n' "hand-written C (cc -O2)" "$C_T"
if [ "$HAVE_RUST" = 1 ]; then
    RS_T="$(best_time "$WORK/fib_rs")"
    printf '| %-26s | %5ss | for reference (rustc -O)\n' "Rust" "$RS_T"
else
    echo "(rustc not found — skipped Rust comparison)"
fi
echo
echo "fib(40) = $EXPECT"
