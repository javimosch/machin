#!/usr/bin/env bash
# Stage-3 sub-slice (3) verifier: user-function CALLS + monomorphization. Diffs
# `machin checktest --program` against the MFL checker on multi-function programs —
# generic helpers specialized per call-site type, multi-return, recursion.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"

echo "building Go machin (oracle) + MFL checker…"
go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
"$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/checkmain.src > /tmp/sh-checker.mfl
"$MACHIN" build /tmp/sh-checker.mfl -o selfhost/mfl-check

T=$(mktemp -d)
pass=0; fail=0; skip=0

run() {
  "$MACHIN" encode "$1" > "$T/x.mfl" 2>/dev/null || return
  m=$(./selfhost/mfl-check --program "$T/x.mfl" 2>/dev/null)
  [ "$m" = "(unsupported)" ] && { skip=$((skip+1)); return; }
  o=$("$MACHIN" checktest --program "$T/x.mfl" 2>/dev/null)
  if [ "$o" = "$m" ]; then pass=$((pass+1)); else
    fail=$((fail+1)); echo "MISMATCH: $1"; echo " O: $o"; echo " M: $m"
  fi
}

# monomorphization: dbl at int and float -> two instances; add deduped; sorted dump
cat > "$T/m1.src" <<'EOF'
func add(a, b) (r) { r = a + b }
func dbl(x) (y) { y = x + x }
func main() {
    p := add(3, 4)
    q := dbl(5)
    f := dbl(1.5)
    g := add(p, q)
}
EOF
# multi-return destructuring + recursion (monomorphic)
cat > "$T/m2.src" <<'EOF'
func divmod(a, b) (q, r) { q = a / b  r = a % b }
func fact(n) (f) {
    if n < 2 { f = 1  return f }
    f = n * fact(n - 1)
}
func main() {
    x, y := divmod(17, 5)
    z := fact(6)
    s := x + y + z
}
EOF
# param type inferred from body usage; call chains across functions
cat > "$T/m3.src" <<'EOF'
func firstlen(xs) (n) { n = 0  for _, v := range xs { n = n + len2(v) } }
func len2(s) (r) { r = 0 }
func main() {
    ss := []string{"a", "bb"}
    total := firstlen(ss)
}
EOF
for f in "$T"/m*.src; do run "$f"; done

# randomized multi-function fuzz (generic helpers at varying types)
for seed in $(seq 1 300); do
  python3 selfhost/gen-check3.py "$seed" $(( (seed % 14) + 4 )) > "$T/g.src"
  run "$T/g.src"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail  (SKIP $skip)"
[ "$fail" -eq 0 ]
