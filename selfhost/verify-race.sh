#!/usr/bin/env bash
# Verifier for the self-hosted data-race pass (racecheck.src): its findings must be
# byte-identical to the Go reference `machin racetest --program`. Slice A covers local
# parameter races (write/write + read/write), type-aware; the race-free concurrency
# corpus must stay clean.
set -u
N="nice -n 15"
MACHIN=./bin/machin
T=$(mktemp -d)
pass=0; fail=0

echo "building Go machin (oracle) + MFL race pass…"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src \
    selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src \
    selfhost/racecheck.src selfhost/racemain.src > "$T/sh-race.mfl" || { echo "encode failed"; exit 1; }
$N "$MACHIN" build "$T/sh-race.mfl" -o selfhost/mfl-race || { echo "build failed"; exit 1; }
echo "built selfhost/mfl-race"

run() {
  $N "$MACHIN" encode "$1" > "$T/x.mfl" 2>/dev/null || { echo "ENCODE-FAIL: $1"; fail=$((fail+1)); return; }
  [ -s "$T/x.mfl" ] || { echo "ENCODE-EMPTY: $1"; fail=$((fail+1)); return; }
  $N "$MACHIN" racetest --program "$T/x.mfl" > "$T/o.txt" 2>/dev/null
  $N ./selfhost/mfl-race --program "$T/x.mfl" > "$T/m.txt" 2>/dev/null
  if diff -q "$T/o.txt" "$T/m.txt" >/dev/null; then pass=$((pass+1)); else
    fail=$((fail+1)); echo "MISMATCH: $1"; diff "$T/o.txt" "$T/m.txt" | head -8
  fi
}

# r1 write/write, r2 read/write, r3 transitive, r4 distinct(clean), r5 scalar-field(clean),
# r6 slice-field(race)
cat > "$T/r1.src" <<'EOF'
func w(xs, i) { xs[i] = i }
func main() { d := []int{0, 0} go w(d, 0) go w(d, 1) }
EOF
run "$T/r1.src"
cat > "$T/r2.src" <<'EOF'
func w(xs) { xs[0] = 9 }
func main() { d := []int{0, 0} go w(d) v := d[1] print(v) }
EOF
run "$T/r2.src"
cat > "$T/r3.src" <<'EOF'
func poke(ys, k) { ys[k] = 1 }
func w(xs, i) { poke(xs, i) }
func main() { d := []int{0, 0} go w(d, 0) go w(d, 1) }
EOF
run "$T/r3.src"
cat > "$T/r4.src" <<'EOF'
func w(xs, i) { xs[i] = i }
func main() { a := []int{0, 0} b := []int{0, 0} go w(a, 0) go w(b, 1) }
EOF
run "$T/r4.src"
cat > "$T/r5.src" <<'EOF'
type Box struct { n int }
func w(b) { b.n = 9 }
func main() { x := Box{0} go w(x) go w(x) }
EOF
run "$T/r5.src"
cat > "$T/r6.src" <<'EOF'
type Bag struct { items []int }
func w(b, i) { b.items[i] = i }
func main() { g := Bag{[]int{0, 0}} go w(g, 0) go w(g, 1) }
EOF
run "$T/r6.src"

# the race-free concurrency corpus must stay clean (empty) on both sides
for app in machin-healthcheck machin-linkcheck machin-pipe machin-pool machin-wscat; do
  s=$(ls ../$app/*.src 2>/dev/null | head -1)
  [ -n "$s" ] && run "$s"
done

echo "----"
echo "PASS $pass  FAIL $fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
