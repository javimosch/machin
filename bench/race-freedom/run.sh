#!/usr/bin/env bash
# The same textbook data race (4 threads incrementing a shared counter,
# 2,000,000 times each, expected sum 8,000,000), three ways. Needs machin, go,
# rustc. See issue #287 and README.md for the honest reading of the results.
set +e
cd "$(dirname "$0")"
FAIL=0

echo "================================================================"
echo "1. machin: does the CHECKER catch the race on the untouched code?"
echo "================================================================"
machin encode racy.src > racy.mfl
machin check --json racy.mfl
if [ $? -eq 0 ]; then
  echo "FAIL: expected machin check to report a race (exit != 0)"; FAIL=1
else
  echo "-> as expected: machin check reports RACE001, exit != 0"
fi

echo
echo "================================================================"
echo "2. machin: does --race-safe REFUSE to build it?"
echo "================================================================"
machin build --race-safe racy.mfl -o racy-race-safe 2>&1
if [ $? -eq 0 ]; then
  echo "FAIL: expected --race-safe to refuse the build"; FAIL=1
else
  echo "-> as expected: --race-safe refuses (exit != 0)"
fi

echo
echo "================================================================"
echo "3. machin: plain build still succeeds (non-breaking) — run it 3x"
echo "   (numeric output is NOT reliable evidence either way — see README)"
echo "================================================================"
machin build racy.mfl -o racy-machin 2>&1
for i in 1 2 3; do ./racy-machin; done

echo
echo "================================================================"
echo "4. Go: go build compiles the SAME race silently — no error, no warning"
echo "================================================================"
go build -o racy-go racy.go 2>&1
if [ $? -ne 0 ]; then
  echo "FAIL: expected go build to succeed silently"; FAIL=1
fi
echo "-> ran 3x (expect 8000000; a wrong number is real, visible corruption):"
for i in 1 2 3; do ./racy-go; done

echo
echo "================================================================"
echo "5. Go: go run -race independently confirms it's a real data race"
echo "   (an OPT-IN dynamic tool, not the compiler — you have to know to ask)"
echo "================================================================"
go run -race racy.go 2>&1 | head -5
echo "..."

echo
echo "================================================================"
echo "6. Rust: the naive translation (no unsafe) — does it even compile?"
echo "================================================================"
rustc -O racy_no_unsafe.rs -o racy_no_unsafe 2>&1 | head -6
if [ -x racy_no_unsafe ]; then
  echo "FAIL: expected this NOT to compile"; FAIL=1
else
  echo "-> as expected: E0133, refuses to compile"
fi

echo
echo "================================================================"
echo "7. Rust: wrap in unsafe (an explicit admission) — compiles, still warns"
echo "================================================================"
rustc -O racy_naive.rs -o racy_naive 2>&1
for i in 1 2 3; do ./racy_naive; done

echo
echo "================================================================"
echo "8. Rust: the actually-safe fix needs Arc<AtomicI64> + Ordering"
echo "================================================================"
rustc -O safe.rs -o safe-rust 2>&1
for i in 1 2 3; do ./safe-rust; done

echo
echo "================================================================"
echo "9. machin: the fix (share by communicating) — zero extra annotations"
echo "================================================================"
machin encode safe.src > safe.mfl
machin check --json safe.mfl
if [ $? -ne 0 ]; then
  echo "FAIL: expected the fixed version to check clean"; FAIL=1
fi
machin build --race-safe safe.mfl -o safe-machin 2>&1
if [ $? -ne 0 ]; then
  echo "FAIL: expected --race-safe to accept the fixed version"; FAIL=1
else
  echo "-> as expected: --race-safe accepts it, zero annotations added"
fi
./safe-machin

echo
if [ $FAIL -ne 0 ]; then
  echo "SOME STEPS DID NOT MATCH THE EXPECTED OUTCOME — see FAIL lines above"
  exit 1
fi
echo "ALL STEPS MATCHED THE EXPECTED OUTCOME."

rm -f racy.mfl safe.mfl racy-race-safe racy-machin racy-go racy_no_unsafe racy_naive safe-rust safe-machin
