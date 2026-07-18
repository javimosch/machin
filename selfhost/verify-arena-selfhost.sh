#!/usr/bin/env bash
# Oracle-diff gate for the SELF-HOSTED arena escape analysis (selfhost/arena.src): its
# ARENA001/ARENA002 findings must be byte-identical to the Go reference
# `machin arenatest --program`. Proves the MFL port reproduces the Go compile-time oracle exactly.
set -u
N="nice -n 15"
MACHIN=./bin/machin
T=$(mktemp -d)
pass=0; fail=0

echo "building Go machin (oracle) + MFL arena pass…"
GOMAXPROCS=4 $N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src \
    selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src \
    selfhost/arena.src selfhost/arenamain.src > "$T/sh.mfl" || { echo "encode failed"; exit 1; }
$N "$MACHIN" build "$T/sh.mfl" -o selfhost/mfl-arena || { echo "build failed"; exit 1; }
echo "built selfhost/mfl-arena"

run() { # file label
  $N "$MACHIN" arenatest --program "$1" > "$T/o.txt" 2>/dev/null
  $N ./selfhost/mfl-arena --program "$1" > "$T/m.txt" 2>/dev/null
  if diff -q "$T/o.txt" "$T/m.txt" >/dev/null; then pass=$((pass+1)); echo "ok   $2"; else
    fail=$((fail+1)); echo "MISMATCH: $2"; echo " oracle:"; cat "$T/o.txt"; echo " mfl:"; cat "$T/m.txt"; fi
}

# ARENA001 escape vectors (a value allocated inside the block reaches an outer-scoped location).
printf 'func f() (r){arena{s:="x"+str(1) r=s}}\nfunc main(){println(f())}\n' > "$T/e1"; run "$T/e1" "named-return escape (ARENA001)"
printf 'func f() (r){x:="" arena{x="a"+str(2)} r=x}\nfunc main(){println(f())}\n' > "$T/e2"; run "$T/e2" "outer-var escape (ARENA001)"
printf 'func f() (r){xs:=[]int{} arena{xs=append(xs,len("q"))} r=str(len(xs))}\nfunc main(){println(f())}\n' > "$T/e3"; run "$T/e3" "append-outer-slice (ARENA001)"
printf 'type P struct{name string}\nfunc f() (r){p:=P{"init"} arena{p.name="n"+str(1)} r=p.name}\nfunc main(){println(f())}\n' > "$T/e4"; run "$T/e4" "struct-field escape (ARENA001)"

# ARENA002: any return inside an arena block.
printf 'func f(n) (r){arena{return "v"+str(n)} r=""}\nfunc main(){println(f(1))}\n' > "$T/r1"; run "$T/r1" "tainted return in arena (ARENA002)"
printf 'func f(n) (r){arena{s:="x"+str(n) println(s) return n} r=0}\nfunc main(){println(str(f(2)))}\n' > "$T/r2"; run "$T/r2" "scalar/bare return in arena (ARENA002)"

# clean: legitimate arena patterns must produce NOTHING.
printf 'func f(n) (r){total:=0 arena{s:="i"+str(n) total=total+len(s)} r=total}\nfunc main(){println(str(f(3)))}\n' > "$T/c1"; run "$T/c1" "scalar accumulator (clean)"
printf 'func f(p) (r){arena{r=p}}\nfunc main(){println(f("hi"))}\n' > "$T/c2"; run "$T/c2" "param passed out (clean)"
printf 'func f(n) (r){arena{s:="x"+str(n) t:=s+"!" println(t)} r="done"}\nfunc main(){println(f(2))}\n' > "$T/c3"; run "$T/c3" "all-inner (clean)"
printf 'func f(ch){arena{ch<-"msg"+str(1)}}\nfunc main(){ch:=make(chan string) go f(ch) println(<-ch)}\n' > "$T/c4"; run "$T/c4" "channel send is safe (clean)"
printf 'func f(a) (r){r=a+1}\nfunc main(){println(str(f(2)))}\n' > "$T/c5"; run "$T/c5" "no arena block (clean)"

# finer place-path granularity: arena memory into an inner container's field/element taints that
# place, so escaping the whole container is caught; extracting a DIFFERENT clean field is not.
printf 'type Box struct{items []string}\nfunc f(seed) (r){arena{p:=Box{seed} p.items=append(p.items,"x"+str(1)) r=p}}\nfunc main(){b:=f([]string{}) println(str(len(b.items)))}\n' > "$T/sf1"; run "$T/sf1" "field-assign into inner struct then escape (ARENA001)"
printf 'func f(seed) (r){arena{xs:=seed xs[0]="a"+str(1) r=xs}}\nfunc main(){b:=f([]string{"z"}) println(b[0])}\n' > "$T/sf2"; run "$T/sf2" "index-assign into inner slice then escape (ARENA001)"
printf 'type Pair struct{a []string  b []string}\nfunc f(s1,s2) (r){arena{p:=Pair{s1,s2} p.a=append(p.a,"x"+str(1)) r=p.b}}\nfunc main(){z:=f([]string{},[]string{"ok"}) println(z[0])}\n' > "$T/sf3"; run "$T/sf3" "extract a different clean field (clean)"

# interprocedural provenance: a pass-through helper must NOT flag; a fresh-allocating one must.
printf 'func idp(s) (r){r=s}\nfunc f(p) (r){x:="" arena{x=idp(p)} r=x}\nfunc main(){println(f("hi"))}\n' > "$T/i1"; run "$T/i1" "pass-through helper (clean)"
printf 'func wrap(s) (r){r="["+s+"]"}\nfunc f(p) (r){x:="" arena{x=wrap(p)} r=x}\nfunc main(){println(f("hi"))}\n' > "$T/i2"; run "$T/i2" "fresh-allocating helper (ARENA001)"

echo
echo "self-hosted arena oracle-diff: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
