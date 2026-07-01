#!/usr/bin/env bash
# The SELF-HOSTING FIXPOINT: the MFL compiler compiles its own source into a native
# binary that reproduces itself.
#   1. build mfl-cgen (the MFL compiler) from its MFL source, via Go machin
#   2. mfl-cgen --full <its own source>  -> mflc2.c  ; cc -> mflc2
#   3. mflc2   --full <the same source>  -> mflc3.c
#   4. assert mflc2.c == mflc3.c   (the fixpoint)
#   5. cross-check: self-compiled mflc2 emits the SAME C as the Go reference for an
#      independent program, and that binary runs correctly.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
export GOMAXPROCS="${GOMAXPROCS:-4}"
N="nice -n 15"
T=$(mktemp -d)
SRCS="selfhost/lex.src selfhost/parse.src selfhost/check.src selfhost/checkgen.src \
selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src selfhost/cgffi.src \
selfhost/cgprelude.src selfhost/cgprog.src selfhost/cgmain.src"

echo "building Go machin + the MFL compiler (mfl-cgen)…"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode $SRCS > "$T/compiler.mfl" || { echo "encode failed"; exit 1; }
$N "$MACHIN" build "$T/compiler.mfl" -o selfhost/mfl-cgen || { echo "mfl-cgen build failed"; exit 1; }

echo "1) mfl-cgen compiles its OWN source -> mflc2.c -> native mflc2"
$N ./selfhost/mfl-cgen --full "$T/compiler.mfl" > "$T/mflc2.c" 2>/dev/null
$N cc -O2 -std=c11 -pthread "$T/mflc2.c" -o "$T/mflc2" || { echo "mflc2 cc failed"; exit 1; }

echo "2) the self-compiled mflc2 re-emits its own source -> mflc3.c"
"$T/mflc2" --full "$T/compiler.mfl" > "$T/mflc3.c" 2>/dev/null

echo "3) FIXPOINT: mflc2.c == mflc3.c ?"
fail=0
if diff -q "$T/mflc2.c" "$T/mflc3.c" >/dev/null; then
  echo "   OK — byte-for-byte identical ($(wc -l < "$T/mflc2.c") lines of C)"
else
  fail=1; echo "   FAIL"; diff "$T/mflc2.c" "$T/mflc3.c" | head
fi

echo "4) cross-check: mflc2 == Go machin codegen on an independent program, and runs"
cat > "$T/demo.src" <<'EOF'
type P struct { x int  y int }
func main() {
    xs := []int{3, 1, 2}
    xs = append(xs, 5)
    p := P{x: 10, y: 20}
    total := p.x + p.y
    for _, v := range xs { total = total + v }
    println(to_upper("hello"), total, len(xs))
}
EOF
$N "$MACHIN" encode "$T/demo.src" > "$T/demo.mfl" 2>/dev/null
$N "$MACHIN" cgentest --program "$T/demo.mfl" > "$T/go_body.c" 2>/dev/null
"$T/mflc2" --program "$T/demo.mfl" > "$T/mflc2_body.c" 2>/dev/null
if diff -q "$T/go_body.c" "$T/mflc2_body.c" >/dev/null; then echo "   codegen matches Go: OK"; else fail=1; echo "   codegen DIFFERS"; fi
"$T/mflc2" --full "$T/demo.mfl" > "$T/demo.c" 2>/dev/null
$N cc -O2 -std=c11 -pthread "$T/demo.c" -o "$T/demo" 2>/dev/null && out=$("$T/demo")
exp=$($N "$MACHIN" run "$T/demo.mfl" 2>/dev/null)
if [ "$out" = "$exp" ]; then echo "   runs correctly: [$out]"; else fail=1; echo "   runtime DIFFERS: [$out] vs [$exp]"; fi

rm -rf "$T"
echo "----"
[ "$fail" -eq 0 ] && echo "SELF-HOSTING FIXPOINT: PASS" || echo "FIXPOINT: FAIL"
[ "$fail" -eq 0 ]
