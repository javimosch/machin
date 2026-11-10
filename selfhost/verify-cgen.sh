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
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/alias.src selfhost/cgagg.src selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src selfhost/cgmain.src > /tmp/sh-cgen.mfl
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

# h10 — struct channels via the string-offset copy path (Slice 3 of #280).
cat > "$T/h10.src" <<'EOF'
type Result struct { url string  status int  label string  ok bool }
func work(ch, u) { r := Result{u, 200, "ok", true}  ch <- r }
func main() {
    ch := make(chan Result)
    go work(ch, "a")
    go work(ch, "bb")
    r1 := <-ch
    r2 := <-ch
    print(r1.status + r2.status)
}
EOF
run "$T/h10.src"

# #293: slice/map (JSON) channels — the element can't be deep-copied by the flat
# string-offset path, so it round-trips through a generated per-type JSON
# serializer/parser instead.
cat > "$T/h11.src" <<'EOF'
func sender(ch) {
    ch <- []int{1, 2, 3}
}
func main() {
    ch := make(chan []int)
    go sender(ch)
    xs := <-ch
    println(str(len(xs)))
}
EOF
run "$T/h11.src"

cat > "$T/h12.src" <<'EOF'
func sender(ch) {
    m := make(map[string]int)
    m["a"] = 1
    ch <- m
}
func main() {
    ch := make(chan map[string]int)
    go sender(ch)
    m := <-ch
    println(str(len(keys(m))))
}
EOF
run "$T/h12.src"

cat > "$T/h13.src" <<'EOF'
type Payload struct { name string  items []int }
func sender(ch) {
    p := Payload{name: "x", items: []int{1, 2}}
    ch <- p
}
func main() {
    ch := make(chan Payload)
    go sender(ch)
    p := <-ch
    println(p.name + " " + str(len(p.items)))
}
EOF
run "$T/h13.src"

# h14 — sqlite_errmsg builtin: signature + mfl_sqlite_errmsg emit parity (#672).
cat > "$T/h14.src" <<'EOF'
func main() {
    db := sqlite_open(":memory:")
    r := sqlite_query(db, "SELECT 1")
    e := sqlite_errmsg(db)
    println(r + "|" + e)
    sqlite_close(db)
}
EOF
run "$T/h14.src"

# h21 — #648: stores into package globals promote arena-backed payload into the
# main arena (string offsets, JSON round-trip for slice/map, struct fields,
# index/field assign roots).
cat > "$T/h21.src" <<'EOF'
type Cfg struct { n int  tag string }
var gname = ""
var glabel = ""
var goflags = []string{}
var gnotes = make(map[string]string)
var gcfg = Cfg{n: 0, tag: ""}
func writer(done) {
    part := "world"
    gname = "hello-" + part
    gname, glabel = "m-" + part, "n-" + part
    goflags = []string{"a" + part, "b" + part}
    gnotes["k"] = "v-" + part
    gcfg = Cfg{n: 7, tag: "t-" + part}
    gcfg.tag = "u-" + part
    goflags[0] = "z" + part
    done <- 1
}
func main() {
    done := make(chan int)
    go writer(done)
    <-done
    sleep(30)
    println(gname)
    println(glabel)
    println(goflags[0] + " " + goflags[1])
    println(gnotes["k"])
    println(str(gcfg.n) + " " + gcfg.tag)
}
EOF
run "$T/h21.src"

# h15 — make([]T, n[, cap]) slice allocation (#678: was a selfhost parse gap).
cat > "$T/h15.src" <<'EOF'
func main() {
    a := make([]int, 4)
    a[0] = 7
    a[3] = 9
    println(len(a), a[0], a[3])
    b := make([]float, 3, 8)
    b[1] = 2.5
    println(len(b), b[1])
    s := make([]string, 2)
    s[0] = "hi"
    println(s[0], len(s))
}
EOF
run "$T/h15.src"

# h16 — `<-` lexer ambiguity (issue #208): `a < -2` tightens to `a<-2` in the
# canonical .mfl and must still parse as less-than + unary minus, while a real
# send `ch <- v` keeps its meaning.
cat > "$T/h16.src" <<'EOF'
func main() {
    a := 5
    b := a < -2
    c := a<-2
    println(b, c)
}
EOF
run "$T/h16.src"

# h17 — float exponent literals (1e-30, 2.5e3, 1E+2).
cat > "$T/h17.src" <<'EOF'
func main() {
    a := 1e-30
    b := 2.5e3
    c := 1E+2
    d := 1e3
    println(a, b, c, d)
}
EOF
run "$T/h17.src"

# h18 — sort(xs): copy-sort builtin (#580). sort_by needs a closure arg, which
# still hits the pre-existing $lambda C-name divergence — covered by hand check.
cat > "$T/h18.src" <<'EOF'
func main() {
    xs := []int{}
    xs = append(xs, 3)
    xs = append(xs, 1)
    xs = append(xs, 2)
    ys := sort(xs)
    println(ys[0], ys[1], ys[2], len(xs))
}
EOF
run "$T/h18.src"
# h19 — full peek/poke width matrix (#675): every signed/unsigned width plus
# f64 and ptr must emit the same mfl_* call in both compilers.
cat > "$T/h19.src" <<'EOF'
func main() {
    p := alloc(64)
    poke_i8(p, 0, -100)
    poke_u16(p, 4, 65535)
    poke_i32(p, 8, -70000)
    poke_u32(p, 12, 4000000000)
    poke_i64(p, 16, -9000000000)
    poke_u64(p, 24, 0x7FFFFFFFFFFFFFFF)
    poke_f32(p, 32, 2.5)
    poke_f64(p, 40, 3.14159)
    poke_ptr(p, 48, p)
    println(peek_i8(p, 0), peek_u16(p, 4), peek_i32(p, 8), peek_u32(p, 12))
    println(peek_i64(p, 16), peek_u64(p, 24), peek_f32(p, 32), peek_f64(p, 40))
    println(peek_ptr(p, 48) == p)
    free(p)
}
EOF
run "$T/h19.src"
# h20 — #650: struct typedefs emit in dependency order (user->cstruct fields,
# forward cstruct->cstruct nesting, converter order).
cat > "$T/h20.src" <<'EOF'
extern "g" {
    header "g.h"
    cstruct Outer { i Inner }
    cstruct Inner { v i32 }
    cstruct Color { r u8 g u8 b u8 a u8 }
    fn Use(Outer, Color)
}
type Wrap struct { o Outer  c Color }
type Theme struct { bg Color }
func main() {
    w := Wrap{}
    t := Theme{}
    Use(w.o, t.bg)
}
EOF
run "$T/h20.src"

# h22 — global initializers may reference earlier globals AND call functions /
# builtins (idents inside make() sizes, arithmetic, Call nodes). The "$globals"
# synthetic context has no FuncDecl instance; Go's isBoxed nil-derefed on it,
# and the selfhost dropped cur_cnodes/cur_cinsts/cur_lit* for iid == -1 lookups.
cat > "$T/h22.src" <<'EOF'
var n = 3
var m = n + 1
var xs = make([]int, n, m * 2)
func dbl(x) { return x * 2 }
var d = dbl(n)
var s = str(n) + "!"
func main() { println(m + len(xs) + d + len(s)) }
EOF
run "$T/h22.src"

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
