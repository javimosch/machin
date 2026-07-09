#!/usr/bin/env bash
# Closure codegen verifier (#308). verify-cgen.sh byte-diffs the self-hosted codegen
# against the Go oracle, but it CANNOT cover closures: the self-hosted compiler names
# lifted lambdas `$lambda_<nodeidx>` and emits a bare callvalue, whereas the reference
# names them `lambda_<counter>` and wraps callvalue in a `_c<id>` temp — two functionally
# equivalent, pre-existing (Phase 1) shape differences. So closures are verified the way
# Phase 1 established: compile + RUN the self-hosted-generated C and diff its runtime
# output against the reference-built binary.
#
# Mechanism: the self-hosted `--program` C is spliced onto the reference's (correct)
# prelude — obtained from `machin build --emit-c` — because the self-hosted `--full`
# prelude blob is stale (a separate, tracked follow-up: `unix` macro clash etc.). The
# program half of --emit-c is byte-identical to `cgentest --program`, so the splice is
# exact for everything except the closure bits under test.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
export GOMAXPROCS="${GOMAXPROCS:-4}"
N="nice -n 15"
CCFLAGS="-O2 -fno-strict-aliasing -std=c11 -pthread -w"

echo "building Go machin (oracle) + MFL codegen…"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src selfhost/cgmain.src > /tmp/shc-cgen.mfl
$N "$MACHIN" build /tmp/shc-cgen.mfl -o selfhost/mfl-cgen

T=$(mktemp -d); pass=0; fail=0

# run <name>: encode, build the reference binary, splice self-hosted program C onto the
# reference prelude, compile + run both, diff runtime output (and exit code).
run() {
  local name="$1"
  $N "$MACHIN" encode "$T/$name.src" > "$T/$name.mfl" 2>/dev/null || { echo "ENCODE FAIL: $name"; fail=$((fail+1)); return; }
  $N "$MACHIN" build --emit-c "$T/$name.mfl" > "$T/$name.full.c" 2>/dev/null
  $N "$MACHIN" cgentest --program "$T/$name.mfl" > "$T/$name.refprog.c" 2>/dev/null
  $N ./selfhost/mfl-cgen --program "$T/$name.mfl" > "$T/$name.shprog.c" 2>/dev/null || { echo "SH-CODEGEN FAIL: $name"; fail=$((fail+1)); return; }
  local plen flen
  plen=$(wc -l < "$T/$name.refprog.c"); flen=$(wc -l < "$T/$name.full.c")
  head -n $((flen - plen)) "$T/$name.full.c" > "$T/$name.prelude.c"
  # guard: the reference full-C's program suffix must equal cgentest --program
  if ! diff -q <(tail -n "$plen" "$T/$name.full.c") "$T/$name.refprog.c" >/dev/null; then
    echo "SPLICE-GUARD FAIL: $name (prelude boundary mismatch)"; fail=$((fail+1)); return
  fi
  cat "$T/$name.prelude.c" "$T/$name.shprog.c" > "$T/$name.sh.c"
  cc $CCFLAGS "$T/$name.sh.c" -o "$T/$name.sh.bin" -lm -lpthread 2>"$T/$name.cc.err" || { echo "CC FAIL: $name"; head -8 "$T/$name.cc.err"; fail=$((fail+1)); return; }
  $N "$MACHIN" build "$T/$name.mfl" -o "$T/$name.ref.bin" 2>/dev/null
  "$T/$name.ref.bin" > "$T/$name.ref.out" 2>&1; local rc_ref=$?
  "$T/$name.sh.bin"  > "$T/$name.sh.out"  2>&1; local rc_sh=$?
  if diff -q "$T/$name.ref.out" "$T/$name.sh.out" >/dev/null && [ "$rc_ref" = "$rc_sh" ]; then
    pass=$((pass+1))
  else
    fail=$((fail+1)); echo "RUNTIME MISMATCH: $name (rc ref=$rc_ref sh=$rc_sh)"
    echo "  ref: $(head -c 200 "$T/$name.ref.out")"; echo "  sh : $(head -c 200 "$T/$name.sh.out")"
  fi
}

# ---- captureless closures (Phase 1 regression: passed as a value + called via callvalue) ----
cat > "$T/cl_hof.src" <<'EOF'
func apply(g, x) (r) { r = g(x) }
func main() {
    println(str(apply(func(z) { return z * 2 }, 21)))
    println(str(apply(func(z) { return z + 100 }, 5)))
}
EOF
run cl_hof

cat > "$T/cl_router.src" <<'EOF'
func main() {
    f := func(n) { return n + 1 }
    g := func(n) { return n * 10 }
    println(str(f(7)))
    println(str(g(7)))
}
EOF
run cl_router

# ---- Phase 2: real variable capture ----
# (a) read-only, multiple captures, sorted env order
cat > "$T/cap_read.src" <<'EOF'
func mk(a, b, c) (f) { f = func() { return a * 100 + b * 10 + c } }
func main() { g := mk(1, 2, 3)  println(str(g())) }
EOF
run cap_read

# (b) two closures sharing one captured variable; mutated by one, read by the other
cat > "$T/cap_share.src" <<'EOF'
func counter() (inc, get) {
    n := 0
    inc = func() { n = n + 1 }
    get = func() { return n }
}
func main() {
    i, g := counter()
    i()  i()  i()
    println(str(g()))
}
EOF
run cap_share

# (c) capture-by-reference: enclosing mutates AFTER the closure is built
cat > "$T/cap_byref.src" <<'EOF'
func main() {
    x := 10
    f := func() { return x }
    x = 99
    println(str(f()))
}
EOF
run cap_byref

# (d) captured variable escapes via `go` (the #314 arena-boundary scenario)
cat > "$T/cap_go.src" <<'EOF'
func run(ch, base) { f := func(y) { return y + base }  ch <- f(5) }
func main() {
    ch := make(chan int)
    go run(ch, 100)
    println(str(<-ch))
}
EOF
run cap_go

# (e) closure-local shadows an enclosing name -> must NOT capture (localNames)
cat > "$T/cap_shadow.src" <<'EOF'
func main() {
    n := 7
    f := func() { n := 3  return n + 1 }
    println(str(f()))
    println(str(n))
}
EOF
run cap_shadow

# (f) string + slice captures (aggregate-typed env fields)
cat > "$T/cap_agg.src" <<'EOF'
func mk(prefix, xs) (f) { f = func() { return prefix + str(len(xs)) } }
func main() {
    g := mk("n=", []int{4, 5, 6})
    println(g())
}
EOF
run cap_agg

# (g) capture read+written across successive calls (accumulator)
cat > "$T/cap_loop.src" <<'EOF'
func mk(start) (step) {
    acc := start
    step = func(d) { acc = acc + d  return acc }
}
func main() {
    s := mk(10)
    println(str(s(1)))
    println(str(s(2)))
    println(str(s(3)))
}
EOF
run cap_loop

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail"
[ "$fail" -eq 0 ]
