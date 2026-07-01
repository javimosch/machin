#!/usr/bin/env bash
# Stage-1 verifier: prove the MFL lexer (selfhost/lex.src) is byte-for-byte
# identical to the Go lexer across the .src corpus. Builds both, diffs every file.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"

echo "building Go machin (oracle) + MFL lexer…"
go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
"$MACHIN" encode selfhost/lex.src > /tmp/sh-lex.mfl
"$MACHIN" build /tmp/sh-lex.mfl -o selfhost/mfl-lex

pass=0; fail=0; toks=0
for f in $(ls framework/*.src 2>/dev/null; find "$HOME/ai" -maxdepth 2 -name '*.src' 2>/dev/null | sort -u); do
  [ -f "$f" ] || continue
  "$MACHIN" lextest "$f" > /tmp/sh-o.txt 2>/dev/null || continue   # skip files the Go lexer rejects
  ./selfhost/mfl-lex "$f" > /tmp/sh-m.txt 2>/dev/null
  if diff -q /tmp/sh-o.txt /tmp/sh-m.txt >/dev/null 2>&1; then
    pass=$((pass+1)); toks=$((toks + $(wc -l < /tmp/sh-o.txt)))
  else
    fail=$((fail+1)); echo "MISMATCH: $f"; diff /tmp/sh-o.txt /tmp/sh-m.txt | head -8
  fi
done
echo "----"
echo "PASS $pass  FAIL $fail  ($toks tokens cross-checked)"
[ "$fail" -eq 0 ]
