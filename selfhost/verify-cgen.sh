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
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src selfhost/cgmain.src > /tmp/sh-cgen.mfl
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

# builtins + printCall + multiAssign (4c)
cat > "$T/h3.src" <<'EOF'
func divmod(a, b) (q, r) { q = a / b  r = a % b }
func main() {
    s := "Hello, World"
    n := len(s)
    up := to_upper(s)
    parts := split(s, ", ")
    j := join(parts, "-")
    f := sqrt(float(n)) * 2.0
    println(up, n, f)
    write(1, j + "\n")
    x := 10
    y := 20
    x, y = y, x
    q, rem := divmod(17, 5)
    println(x, y, q, rem, has_prefix(s, "He"))
}
EOF
run "$T/h3.src"

# aggregates (4d part 1): structs, slices, maps, index/field/range, a package global
cat > "$T/h4.src" <<'EOF'
type Pt struct { x int  y int  name string }
var total = 0
func main() {
    p := Pt{x: 1, y: 2, name: "o"}
    q := Pt{5, 6, "z"}
    p.y = p.x + q.y
    xs := []int{10, 20, 30}
    xs[0] = 99
    for i, v := range xs { total = total + v + i }
    m := make(map[string]int)
    m["k"] = 5
    b := m["k"]
    for key, val := range m { total = total + val + len(key) }
    ss := []string{"a", "bb"}
    for _, s := range ss { total = total + len(s) }
}
EOF
run "$T/h4.src"

# aggregate package-global initializers ([]T{}, struct literals)
cat > "$T/h5.src" <<'EOF'
type Node struct { kind int  s string }
var nodes = []Node{}
var nums = []int{1, 2, 3}
var names = []string{"a", "b"}
var count = 0
func main() {
    nodes = append(nodes, Node{kind: 1, s: "x"})
    count = len(nodes) + len(nums) + len(names)
}
EOF
run "$T/h5.src"

# FFI (4d part 3): cstructs (header + no-header), extern calls, marshaling, ptr, inout
cat > "$T/h6.src" <<'EOF'
extern "raylib" {
    header "raylib.h"
    cstruct Vector3 { x f32  y f32  z f32 }
    cstruct Camera3D { position Vector3  target Vector3  fovy f32  projection i32 }
    fn InitWindow(i32, i32, string)
    fn GetMousePosition() Vector3
    fn Vector3Add(Vector3, Vector3) Vector3
}
extern "mylib" {
    cstruct Buf { data ptr  len i32 }
    fn make_buf() Buf
    fn fill(Buf*, i32)
    fn raw_ptr() ptr
}
func main() {
    InitWindow(800, 600, "demo")
    p := Vector3{x: 1.0, y: 2.0, z: 3.0}
    q := GetMousePosition()
    r := Vector3Add(p, q)
    d := r.x + q.y
    cam := Camera3D{position: p, fovy: 45.0, projection: 0}
    f := cam.fovy
    b := make_buf()
    fill(b, 42)
    n := b.len
    ptr := raw_ptr()
}
EOF
run "$T/h6.src"

# h7 — concurrency: go + scalar channels (trampoline + make/send/recv). Slice 1 of #280.
cat > "$T/h7.src" <<'EOF'
func worker(ch, n) { ch <- n * 2 }
func producer(ch) { ch <- true }
func main() {
    ch := make(chan int)
    go worker(ch, 21)
    go worker(ch, 10)
    a := <-ch
    b := <-ch
    bc := make(chan bool)
    go producer(bc)
    v, ok := <-bc
    if ok { print(a + b) }
    print(v)
}
EOF
run "$T/h7.src"

# h8 — string channels + range over a channel (Slice 2 of #280).
cat > "$T/h8.src" <<'EOF'
func gen(ch) { ch <- "a"  ch <- "b"  close(ch) }
func nums(ch) { ch <- 5  ch <- 7  close(ch) }
func main() {
    sc := make(chan string)
    go gen(sc)
    for s := range sc { print(s) }
    nc := make(chan int)
    go nums(nc)
    total := 0
    for v := range nc { total = total + v }
    print(total)
    dc := make(chan int)
    go nums(dc)
    cnt := 0
    for _ := range dc { cnt = cnt + 1 }
    print(cnt)
}
EOF
run "$T/h8.src"

# h9 — select: recv/send/default/comma-ok cases (Slice 4 of #280).
cat > "$T/h9.src" <<'EOF'
func feed(ch) { ch <- 9  close(ch) }
func drain(ch) { x := <-ch  print(x) }
func main() {
    a := make(chan int)
    b := make(chan int)
    go feed(a)
    go drain(b)
    select {
    case x, ok := <-a:
        if ok { print(x) }
    case b <- 5:
        print(1)
    default:
        print(0)
    }
    c := make(chan int)
    go feed(c)
    select {
    case v := <-c:
        print(v)
    case <-a:
        print(7)
    }
}
EOF
run "$T/h9.src"

# SELF-APPLICATION: the MFL codegen emits byte-identical C for the compiler's OWN
# source (checker + full codegen) — the fixpoint-adjacent milestone.
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/checkmain.src > "$T/self-checker.mfl" 2>/dev/null
run "$T/self-checker.mfl"
cp /tmp/sh-cgen.mfl "$T/self-cgen.mfl" 2>/dev/null && run "$T/self-cgen.mfl"

# randomized fuzz: call-free (4a) + call-heavy (4b) + builtin-heavy (4c) + aggregates (4d)
for seed in $(seq 1 100); do
  python3 selfhost/gen-cg.py "$seed" $(( (seed % 16) + 4 )) > "$T/g.src";  run "$T/g.src"
  python3 selfhost/gen-cg2.py "$seed" $(( (seed % 12) + 4 )) > "$T/g2.src"; run "$T/g2.src"
  python3 selfhost/gen-cg3.py "$seed" $(( (seed % 14) + 4 )) > "$T/g3.src"; run "$T/g3.src"
  python3 selfhost/gen-cg4.py "$seed" $(( (seed % 14) + 5 )) > "$T/g4.src"; run "$T/g4.src"
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail"
[ "$fail" -eq 0 ]
