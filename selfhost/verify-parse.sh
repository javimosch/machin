#!/usr/bin/env bash
# Stage-2 verifier (parser): diff the MFL parser's AST dump against the Go parser
# (`machin parsetest`). Two layers:
#   1. an expression battery (--expr)
#   2. self-application: encode complete programs and diff every function (--funcs),
#      including the parser parsing its own source.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"

go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
"$MACHIN" encode selfhost/lex.src selfhost/parse.src > /tmp/sh-parse.mfl
"$MACHIN" build /tmp/sh-parse.mfl -o selfhost/mfl-parse

fail=0

# --- layer 1: expression battery ---
exprs=(
  '1 + 2 * 3' '(1 + 2) * 3' 'a && b || c && !d' '1 << 4 | 0xff & 0b1010'
  '0o17' '-x + -5' 'foo(1, 2, bar(3))' 'a.b.c' 'm["key"]' 'xs[i+1]'
  'fs[i](a, b)' 'spread(args...)' 'g()(x)' '"a\nb\"c"' '[]int{1, 2, 3}'
  '[]string{}' 'true && false' 'nil' '<-ch' 'a.b[c].d(e)' '3.14' '1.0 / 3.0'
  '0.00001' 'a||b||c||d' 'f(g(h(i(1))))'
)
ep=0
for e in "${exprs[@]}"; do
  o=$("$MACHIN" parsetest --expr "$e" 2>/dev/null); m=$(./selfhost/mfl-parse --expr "$e" 2>/dev/null)
  if [ "$o" = "$m" ] && [ -n "$o" ]; then ep=$((ep+1)); else fail=$((fail+1)); printf 'EXPR MISMATCH: %s\n  o:%s\n  m:%s\n' "$e" "$o" "$m"; fi
done
echo "expressions: $ep/${#exprs[@]}"

# --- layer 2: self-application + any encodable program in the repo ---
mkdir -p /tmp/sh-corpus; rm -f /tmp/sh-corpus/*.mfl
cp /tmp/sh-parse.mfl /tmp/sh-corpus/selfhost.mfl           # the parser's own source
i=0
for f in $(find . -maxdepth 3 -name '*.src' 2>/dev/null | sort -u); do
  out="/tmp/sh-corpus/p$i.mfl"
  "$MACHIN" encode "$f" > "$out" 2>/dev/null
  [ -s "$out" ] || rm -f "$out"; i=$((i+1))
done
fp=0; funcs=0
for mfl in /tmp/sh-corpus/*.mfl; do
  "$MACHIN" parsetest --funcs "$mfl" > /tmp/sh-o.txt 2>/dev/null
  timeout 20 ./selfhost/mfl-parse --funcs "$mfl" > /tmp/sh-m.txt 2>/dev/null
  funcs=$((funcs + $(grep -cE '^\(func ' /tmp/sh-o.txt)))
  if diff -q /tmp/sh-o.txt /tmp/sh-m.txt >/dev/null 2>&1; then fp=$((fp+1)); else fail=$((fail+1)); echo "FUNCS MISMATCH: $(basename "$mfl")"; fi
done
echo "programs: $fp files, $funcs functions cross-checked"
echo "----"; [ "$fail" -eq 0 ] && echo "PASS" || echo "FAIL ($fail)"
[ "$fail" -eq 0 ]
