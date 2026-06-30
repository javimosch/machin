#!/usr/bin/env bash
# Stage-3 sub-slice (2) verifier: prove the MFL constraint generator + solver
# (checkgen.src) infers the same types as the Go checker on MAIN-ONLY programs.
# Diffs `machin checktest --program` against the composed MFL checker (mfl-check).
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

run() { # <src-file>
  "$MACHIN" encode "$1" > "$T/x.mfl" 2>/dev/null || return
  m=$(./selfhost/mfl-check --program "$T/x.mfl" 2>/dev/null)
  [ "$m" = "(unsupported)" ] && { skip=$((skip+1)); return; }
  o=$("$MACHIN" checktest --program "$T/x.mfl" 2>/dev/null)
  if [ "$o" = "$m" ]; then pass=$((pass+1)); else
    fail=$((fail+1)); echo "MISMATCH: $1"; echo " O: $o"; echo " M: $m"
  fi
}

# hand-written cases: every operator, flex int->float back-prop, control flow, slices
cat > "$T/h1.src" <<'EOF'
func main() {
    a := 5
    b := a + 3
    c := 1.5
    d := c * 2.0
    e := a < b
    f := "hi"
    g := f + "x"
    h := a % 2
    i := !e
    j := a & 7
}
EOF
cat > "$T/h2.src" <<'EOF'
func main() {
    x := 5
    y := x + 1.5
    z := x * 2
}
EOF
cat > "$T/h3.src" <<'EOF'
func main() {
    xs := []int{1, 2, 3}
    ys := []string{"a", "b"}
    zs := []float{1.0}
    ns := [][]int{}
    i := 0
    s := ""
    while i < 3 { s = s + "x"  i = i + 1 }
    if i == 3 { k := i << 1 | 1 }
}
EOF
for f in "$T"/h*.src; do run "$f"; done

# randomized type-correct fuzz
for seed in $(seq 1 200); do
  python3 selfhost/gen-check.py "$seed" $(( (seed % 16) + 4 )) > "$T/g.src"
  run "$T/g.src"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail  (SKIP $skip)"
[ "$fail" -eq 0 ]
