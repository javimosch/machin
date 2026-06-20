#!/usr/bin/env bash
#
# Reproducible fib(40) benchmark — backs the numbers in the README "Performance"
# table (see issue #5). Builds the MFL compiler, then times three equivalent
# implementations of naive `fib(40)` under the same conditions:
#
#   1. MFL  — compiled to native via `machin build` (emits C, then `cc -O2`)
#   2. C    — examples/bench/fib.c, compiled with `cc -O2`
#   3. Rust — examples/bench/fib.rs, compiled with `rustc -O` (skipped if absent)
#
# The C and Rust reference sources are checked into examples/bench/ and kept
# equivalent to examples/bench/fib.mfl, so anyone can read them and re-run
# `scripts/bench.sh` to verify the comparison on their own hardware.
#
# Usage: scripts/bench.sh
#
# Fixed at fib(40) to match examples/bench/fib.mfl (whose argument is baked in);
# the C and Rust references default to 40 as well, so all three agree.
set -euo pipefail

N=40
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN="bin/machin"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

CC="${CC:-cc}"

# --- timing helper: best-of-3 wall-clock seconds for "$@" ---
time_best() {
  local best="" t
  for _ in 1 2 3; do
    local start end
    start=$(date +%s.%N)
    "$@" >/dev/null
    end=$(date +%s.%N)
    t=$(awk "BEGIN{printf \"%.3f\", $end - $start}")
    if [ -z "$best" ] || awk "BEGIN{exit !($t < $best)}"; then best="$t"; fi
  done
  echo "$best"
}

echo "Building MFL compiler..."
go build -o "$BIN" . >/dev/null

# --- 1. MFL ---
echo "Compiling MFL fib($N)..."
"$BIN" build examples/bench/fib.mfl -o "$WORK/fib_mfl" >/dev/null
MFL_OUT=$("$WORK/fib_mfl")
MFL_T=$(time_best "$WORK/fib_mfl")
MFL_SIZE=$(wc -c < "$WORK/fib_mfl" | tr -d ' ')

# --- 2. hand-written C ---
echo "Compiling C fib($N)..."
"$CC" -O2 examples/bench/fib.c -o "$WORK/fib_c"
C_OUT=$("$WORK/fib_c" "$N")
C_T=$(time_best "$WORK/fib_c" "$N")

# --- 3. Rust (optional) ---
RUST_T="n/a"
RUST_OUT="(skipped)"
if command -v rustc >/dev/null 2>&1; then
  echo "Compiling Rust fib($N)..."
  rustc -O examples/bench/fib.rs -o "$WORK/fib_rs" 2>/dev/null
  RUST_OUT=$("$WORK/fib_rs" "$N")
  RUST_T=$(time_best "$WORK/fib_rs" "$N")
else
  echo "rustc not found — skipping Rust comparison."
fi

# Sanity: every implementation must agree on the result.
for pair in "C:$C_OUT" "Rust:$RUST_OUT"; do
  name="${pair%%:*}"; got="${pair#*:}"
  if [ "$got" != "(skipped)" ] && [ "$got" != "$MFL_OUT" ]; then
    echo "ERROR: $name produced $got, expected $MFL_OUT (matching MFL)" >&2
    exit 1
  fi
done

echo
echo "fib($N) = $MFL_OUT  (best-of-3 wall-clock, $(uname -m), $($CC --version 2>/dev/null | head -1))"
echo
echo "| Implementation            | Time   | Notes                              |"
echo "|---------------------------|--------|------------------------------------|"
echo "| **MFL** (native, cc -O2)  | ${MFL_T}s | emits C, optimized by the system compiler |"
echo "| hand-written C (cc -O2)   | ${C_T}s | the baseline MFL compiles to       |"
RUST_CELL="${RUST_T}s"; [ "$RUST_T" = "n/a" ] && RUST_CELL="n/a (rustc absent)"
echo "| Rust (rustc -O)           | ${RUST_CELL} | for reference                      |"
echo
echo "MFL binary size: ${MFL_SIZE} bytes"
