#!/usr/bin/env bash
# Stage-3 (sub-slice 1) verifier: prove the MFL union-find ENGINE (selfhost/check.src)
# is byte-for-byte identical to the Go type-checker engine (newSlot/find/union/
# reconcile) over a battery of scripted op sequences — edge cases + randomized fuzz.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"

echo "building Go machin (oracle) + MFL engine…"
go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
"$MACHIN" encode selfhost/check.src selfhost/ufmain.src > /tmp/sh-check.mfl
"$MACHIN" build /tmp/sh-check.mfl -o selfhost/mfl-uf

T=$(mktemp -d)
pass=0; fail=0

run() { # <script-file>
  if diff -q <("$MACHIN" uftest "$1") <(./selfhost/mfl-uf "$1") >/dev/null 2>&1; then
    pass=$((pass+1))
  else
    fail=$((fail+1)); echo "MISMATCH: $1"; diff <("$MACHIN" uftest "$1") <(./selfhost/mfl-uf "$1") | head -12
  fi
}

# hand-written edge cases
printf 'var\nvar\nvar\nvar\nvar\nunion 0 1\nunion 1 2\nunion 2 3\nunion 3 4\ndump\n' > "$T/e1"
printf 'int\nvar\nslice 0\nslice 1\nunion 2 3\ndump\n' > "$T/e2"
printf 'num\nvar\nint\nstring\nmap 0 1\nmap 2 3\nunion 4 5\ndump\n' > "$T/e3"
printf 'var\nvar\nvar\nvar\nfunc 0,1|2\nfunc 3,3|3\nunion 4 5\ndump\n' > "$T/e4"
printf 'struct Foo\nstruct Bar\nunion 0 1\ndump\n' > "$T/e5"
printf 'var\nvar\nfunc 0|1\nfunc 0,1|1\nunion 2 3\ndump\n' > "$T/e6"
for f in "$T"/e*; do run "$f"; done

# randomized fuzz (deterministic per seed)
for seed in $(seq 1 200); do
  ns=$(( (seed % 50) + 5 )); nu=$(( (seed % 40) + 5 ))
  python3 selfhost/gen-uf.py "$seed" "$ns" "$nu" > "$T/r"
  run "$T/r"
done
# a few large ones
for seed in 1 2 3 4 5; do
  python3 selfhost/gen-uf.py "$seed" 300 500 > "$T/b"
  run "$T/b"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail"
[ "$fail" -eq 0 ]
