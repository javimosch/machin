#!/usr/bin/env bash
# THE NO-GO BOOTSTRAP: machin is written in machin, full stop. Starting from a machin
# binary + a C compiler (NO Go), the toolchain reproduces itself byte-for-byte and
# builds arbitrary programs. Go is used ONCE to mint the seed binary M0; everything
# after uses only machin binaries — proving Go is a replaceable bootstrap origin, not
# "under the hood".
#   1. (seed) Go machin builds the standalone MFL `machin` -> M0   [the only Go step]
#   2. M0 encodes its OWN source -> A.mfl, and builds it -> M1     [no Go]
#   3. M1 re-encodes the source -> B.mfl ; assert A.mfl == B.mfl   (encode fixpoint)
#   4. M1 emits its C -> CB.c ; assert CA.c == CB.c                (codegen fixpoint)
#   5. M1 builds + runs a fresh program; its C matches the Go reference (cross-check)
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
export GOMAXPROCS="${GOMAXPROCS:-4}"
N="nice -n 15"
SRCS="selfhost/lex.src selfhost/parse.src selfhost/check.src selfhost/checkgen.src \
selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src selfhost/cgffi.src \
selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src selfhost/encode.src \
selfhost/build.src selfhost/machin.src"
T=$(mktemp -d); fail=0

echo "0) seed: Go machin builds the standalone MFL machin -> M0  (the ONLY Go step)"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode $SRCS > "$T/seed.mfl" || { echo "seed encode failed"; exit 1; }
$N "$MACHIN" build "$T/seed.mfl" -o "$T/M0" || { echo "M0 build failed"; exit 1; }
echo "   --- from here on: no Go, only machin binaries + cc ---"

echo "1) M0 encodes its own source -> A.mfl ; builds -> M1 (+ emit C)"
$N "$T/M0" encode $SRCS > "$T/A.mfl"
$N "$T/M0" build "$T/A.mfl" --emit-c > "$T/CA.c"
$N "$T/M0" build "$T/A.mfl" -o "$T/M1" || { echo "   M1 build failed"; exit 1; }

echo "2) encode fixpoint: M1 re-encodes -> B.mfl ; A == B ?"
$N "$T/M1" encode $SRCS > "$T/B.mfl"
if cmp -s "$T/A.mfl" "$T/B.mfl"; then echo "   OK ($(wc -l < "$T/A.mfl") lines)"; else fail=1; echo "   FAIL"; diff "$T/A.mfl" "$T/B.mfl" | head; fi

echo "3) codegen fixpoint: M1 emits C -> CB.c ; CA == CB ?"
$N "$T/M1" build "$T/B.mfl" --emit-c > "$T/CB.c"
if cmp -s "$T/CA.c" "$T/CB.c"; then echo "   OK ($(wc -l < "$T/CA.c") lines of C)"; else fail=1; echo "   FAIL"; diff "$T/CA.c" "$T/CB.c" | head; fi

echo "4) cross-check: M1 builds + runs a fresh program, C matches the Go reference"
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
$N "$T/M1" encode "$T/demo.src" > "$T/demo.mfl"
$N "$T/M1" build "$T/demo.mfl" -o "$T/demo" && out=$("$T/demo")
exp=$($N "$MACHIN" run "$T/demo.mfl" 2>/dev/null)
if [ "$out" = "$exp" ]; then echo "   runs correctly: [$out]"; else fail=1; echo "   runtime DIFFERS: [$out] vs [$exp]"; fi
$N "$T/M1" build "$T/demo.mfl" --emit-c > "$T/m1.c"
$N "$MACHIN" build "$T/demo.mfl" --emit-c > "$T/go.c" 2>/dev/null
if cmp -s "$T/m1.c" "$T/go.c"; then echo "   codegen == Go reference: OK"; else fail=1; echo "   codegen DIFFERS from Go"; fi

rm -rf "$T"
echo "----"
[ "$fail" -eq 0 ] && echo "NO-GO BOOTSTRAP: PASS — machin is written in machin." || echo "NO-GO BOOTSTRAP: FAIL"
[ "$fail" -eq 0 ]
