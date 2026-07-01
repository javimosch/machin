#!/usr/bin/env bash
# Stage-4 sub-slice (a) verifier: the MFL C codegen (cgen.src/cgprog.src) emits
# byte-for-byte the same program-specific C as the Go codegen (`machin cgentest`,
# bodyOnly) for the CALL-FREE scalar + control-flow subset.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
export GOMAXPROCS="${GOMAXPROCS:-4}"
N="nice -n 15"

echo "building Go machin (oracle) + MFL codegen…"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgprog.src selfhost/cgmain.src > /tmp/sh-cgen.mfl
$N "$MACHIN" build /tmp/sh-cgen.mfl -o selfhost/mfl-cgen

T=$(mktemp -d); pass=0; fail=0

run() {
  $N "$MACHIN" encode "$1" > "$T/x.mfl" 2>/dev/null || return
  $N "$MACHIN" cgentest --program "$T/x.mfl" > "$T/o.txt" 2>/dev/null
  $N ./selfhost/mfl-cgen --program "$T/x.mfl" > "$T/m.txt" 2>/dev/null
  if diff -q "$T/o.txt" "$T/m.txt" >/dev/null; then pass=$((pass+1)); else
    fail=$((fail+1)); echo "MISMATCH: $1"; diff "$T/o.txt" "$T/m.txt" | head -12
  fi
}

# hand cases: every operator, string concat/compare, if/else, while/break/continue, float
cat > "$T/h1.src" <<'EOF'
func main() {
    a := 5
    b := a + 3
    c := "hi"
    d := c + "!"
    e := a < b && b > 0
    f := c == "hi"
    if a < b { g := a % 2 } else { g := b | 1 }
    i := 0
    while i < 10 { i = i + 1  if i == 5 { continue }  if i > 8 { break } }
    x := 1.5 * 2.0
    y := x + 0.25
    z := (-a)
    w := (^a)
}
EOF
run "$T/h1.src"

# calls: monomorphization, nested calls + seqExprs ordering, recursion
cat > "$T/h2.src" <<'EOF'
func add(a, b) (r) { r = a + b }
func dbl(x) (y) { y = x + x }
func fib(n) (f) { if n < 2 { f = n  return f }  f = fib(n - 1) + fib(n - 2) }
func main() {
    p := add(dbl(3), fib(10))
    q := add(add(1, 2), dbl(p))
    s := dbl2("a") + "z"
}
func dbl2(x) (y) { y = x + x }
EOF
run "$T/h2.src"

# randomized fuzz: call-free (4a) + call-heavy (4b)
for seed in $(seq 1 150); do
  python3 selfhost/gen-cg.py "$seed" $(( (seed % 16) + 4 )) > "$T/g.src"
  run "$T/g.src"
  python3 selfhost/gen-cg2.py "$seed" $(( (seed % 12) + 4 )) > "$T/g2.src"
  run "$T/g2.src"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail"
[ "$fail" -eq 0 ]
