#!/usr/bin/env bash
# Build the same four kernels in machin, Rust, and Zig — each at its standard
# release optimization — then verify identical output and time them.
#
#   machin : machin build  (cc -O2, machin's only setting)
#   Rust   : rustc -C opt-level=3   (Rust's real release level — no sandbagging)
#   Zig    : -OReleaseFast          (Zig's fastest mode)
#
# No -march=native for anyone (machin doesn't use it, so neither do the others).
set +e
cd "$(dirname "$0")"
KERNELS="fib mandel sieve intsum"

echo "== build machin (cc -O2) =="
for k in $KERNELS; do
  machin encode machin/$k.src > machin/$k.mfl 2>/dev/null \
    && machin build machin/$k.mfl -o machin/$k 2>/dev/null && echo "  machin/$k ok" || echo "  machin/$k FAIL"
done

echo "== build Rust (rustc -C opt-level=3) =="
for k in $KERNELS; do rustc -C opt-level=3 rust/$k.rs -o rust/$k 2>/dev/null && echo "  rust/$k ok" || echo "  rust/$k FAIL"; done

echo "== build Zig (-OReleaseFast) =="
( cd zig && for k in $KERNELS; do zig build-exe -OReleaseFast $k.zig 2>/dev/null && echo "  zig/$k ok" || echo "  zig/$k FAIL"; done )

echo "== verify identical output =="
for k in $KERNELS; do
  m=$(./machin/$k 2>&1); r=$(./rust/$k 2>&1); z=$(./zig/$k 2>&1)
  [ "$m" = "$r" ] && [ "$r" = "$z" ] && echo "  $k: $m (match)" || echo "  $k: MISMATCH m=$m r=$r z=$z"
done

echo "== timing =="
python3 measure.py
