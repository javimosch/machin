#!/usr/bin/env bash
#
# Reproducible fib(40) benchmark — backs the numbers in the README "Performance"
# table (see issue #5). Builds the MFL compiler, then times three equivalent
# implementations of naive `fib(40)` under the same conditions:
#
#   1. MFL  — compiled to native via `machin build` (emits C, then `cc -O2`)
#   2. C    — hand-written, compiled with `cc -O2`
#   3. Rust — compiled with `rustc -O` (skipped if rustc is absent)
#
# Output is a Markdown table you can paste straight into the README. Because the
# C and Rust sources are generated here, anyone can re-run `scripts/bench.sh` and
# verify the comparison on their own hardware.
#
# Usage: scripts/bench.sh [N]   (default N=40)
set -euo pipefail

N="${1:-40}"
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
cat > "$WORK/fib.c" <<EOF
#include <stdio.h>
long fib(long n){ if(n<2) return n; return fib(n-1)+fib(n-2); }
int main(void){ printf("%ld\n", fib($N)); return 0; }
EOF
"$CC" -O2 "$WORK/fib.c" -o "$WORK/fib_c"
C_T=$(time_best "$WORK/fib_c")

# --- 3. Rust (optional) ---
RUST_T="n/a"
if command -v rustc >/dev/null 2>&1; then
  echo "Compiling Rust fib($N)..."
  cat > "$WORK/fib.rs" <<EOF
fn fib(n: u64) -> u64 { if n < 2 { n } else { fib(n-1) + fib(n-2) } }
fn main(){ println!("{}", fib($N)); }
EOF
  rustc -O "$WORK/fib.rs" -o "$WORK/fib_rs" 2>/dev/null
  RUST_T=$(time_best "$WORK/fib_rs")
else
  echo "rustc not found — skipping Rust comparison."
fi

echo
echo "fib($N) = $MFL_OUT  (best-of-3 wall-clock, $(uname -m), $($CC --version 2>/dev/null | head -1))"
echo
echo "| Implementation            | Time   | Notes                              |"
echo "|---------------------------|--------|------------------------------------|"
echo "| **MFL** (native, cc -O2)  | ${MFL_T}s | emits C, optimized by the system compiler |"
echo "| hand-written C (cc -O2)   | ${C_T}s | the baseline MFL compiles to       |"
echo "| Rust (rustc -O)           | ${RUST_T}s | for reference                      |"
echo
echo "MFL binary size: ${MFL_SIZE} bytes"
