#!/usr/bin/env bash
# Stage-2 verifier (expression parser): diff the MFL parser's AST dump against the
# Go parser (`machin parsetest --expr`) over a battery of expression forms.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"

go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
"$MACHIN" encode selfhost/lex.src selfhost/parse.src > /tmp/sh-parse.mfl
"$MACHIN" build /tmp/sh-parse.mfl -o selfhost/mfl-parse

exprs=(
  '1 + 2 * 3'            '1 + 2 + 3'            '(1 + 2) * 3'
  'a && b || c && !d'    'x == 1 || y != 2'    '1 << 4 | 0xff & 0b1010'
  '0xFF'                 '0o17'                 '-x + -5'
  'foo(1, 2, bar(3))'    'a.b.c'                'm["key"]'
  'xs[i+1]'              'p.x + q.y'            'obj.method(1, 2)'
  'fs[i](a, b)'          'spread(args...)'      'g()(x)'
  '"hello\nworld"'       '"tab\tquote\"end"'    '[]int{1, 2, 3}'
  '[]string{}'           'true && false'        'nil'
  '<-ch'                 'a - b - c'            'len(xs) > 0 && xs[0] == 42'
  'a.b[c].d(e)'          'foo(bar(baz(1)))'     '!(a == b) || c'
  '3.14'                 '1.5 + 2.0'            '1.0 / 3.0'
  '100.0'                '2.0 * 3.0 + 1.5'      'a||b||c||d||e'
  'f(g(h(i(j(1)))))'     '((((1))))'
)
pass=0; fail=0
for e in "${exprs[@]}"; do
  o=$("$MACHIN" parsetest --expr "$e" 2>/dev/null)
  m=$(./selfhost/mfl-parse --expr "$e" 2>/dev/null)
  if [ "$o" = "$m" ] && [ -n "$o" ]; then pass=$((pass+1))
  else fail=$((fail+1)); printf 'MISMATCH: %s\n  oracle: %s\n  mfl   : %s\n' "$e" "$o" "$m"; fi
done
echo "----"; echo "PASS $pass  FAIL $fail"
[ "$fail" -eq 0 ]
