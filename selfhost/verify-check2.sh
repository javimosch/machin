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
# deferred grammar: index / index-assign / range (slice,map,string) / maps
cat > "$T/h4.src" <<'EOF'
func main() {
    xs := []int{10, 20, 30}
    a := xs[0]
    xs[1] = 99
    total := 0
    for i, v := range xs { total = total + v + i }
    m := make(map[string]int)
    m["k"] = 5
    b := m["k"]
    for key, val := range m { c := key }
}
EOF
# structs: literal (named + positional), field access, field assign, slice-of-struct
cat > "$T/h5.src" <<'EOF'
type Point struct { x int  y int  label string }
func main() {
    p := Point{x: 1, y: 2, label: "o"}
    q := Point{5, 6, "z"}
    dx := p.x
    lbl := q.label
    p.y = 42
    pts := []Point{p, q}
    first := pts[0].x
    fl := pts[1].label
}
EOF
# channels: make / send / recv / range over string
cat > "$T/h6.src" <<'EOF'
func main() {
    ch := make(chan int)
    ch <- 7
    got := <-ch
    s := "hello"
    n := 0
    for idx, chr := range s { n = n + idx }
    fc := make(chan float)
    fc <- 1.5
}
EOF
for f in "$T"/h*.src; do run "$f"; done

# randomized type-correct fuzz
for seed in $(seq 1 300); do
  python3 selfhost/gen-check.py "$seed" $(( (seed % 16) + 4 )) > "$T/g.src"
  run "$T/g.src"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail  (SKIP $skip)"
[ "$fail" -eq 0 ]
