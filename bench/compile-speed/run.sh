#!/usr/bin/env bash
# Build the same four kernels in machin, Rust, and Zig — and time the BUILD.
#
# bench/native-speed times the program. This times the compiler: source ->
# runnable optimized binary, which is what you actually wait on in a
# write -> build -> fix loop.
#
# Fairness (see measure.py for the full rationale):
#   * Zig's .zig-cache is deleted before every timed build (otherwise every run
#     after the first is a cache hit, not a build).
#   * Rust is bare `rustc`, not `cargo` — no manifest/lockfile overhead charged
#     to the compiler.
#   * machin's number includes its `cc -O2` backend run — the whole pipeline.
set +e
cd "$(dirname "$0")"

for t in machin rustc zig python3; do
  command -v "$t" >/dev/null || { echo "missing: $t"; exit 1; }
done

echo "== toolchains =="
echo "  machin $(machin guide 2>/dev/null | head -c 200 | grep -o '"version":"[^"]*"' | head -1)"
echo "  $(rustc --version)"
echo "  zig $(zig version)"
echo "  $(cc --version | head -1)"
echo

python3 measure.py
