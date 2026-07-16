#!/usr/bin/env bash
# Oracle-diff gate for the SELF-HOSTED falsify pass (selfhost/falsify.src): its
# findings must be byte-identical to the Go reference `machin falsifytest --program`.
#
# (Distinct from the repo-root verify-falsify.sh, which behaviorally tests the Go
# pass. This one proves the MFL port reproduces the Go oracle exactly.)
#
# Phase 2 slice 2.1: scalar-int params, div/modulo-by-zero (FALS002). Slices,
# index-OOB (FALS001), structs and interprocedural inlining land in later slices.
set -u
N="nice -n 15"
MACHIN=./bin/machin
T=$(mktemp -d)
pass=0; fail=0

echo "building Go machin (oracle) + MFL falsify pass…"
GOMAXPROCS=4 $N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src \
    selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src \
    selfhost/falsify.src selfhost/falsifymain.src > "$T/sh-falsify.mfl" || { echo "encode failed"; exit 1; }
$N "$MACHIN" build "$T/sh-falsify.mfl" -o selfhost/mfl-falsify || { echo "build failed"; exit 1; }
echo "built selfhost/mfl-falsify"

run() { # file label
  $N "$MACHIN" falsifytest --program "$1" > "$T/o.txt" 2>/dev/null
  $N ./selfhost/mfl-falsify --program "$1" > "$T/m.txt" 2>/dev/null
  if diff -q "$T/o.txt" "$T/m.txt" >/dev/null; then pass=$((pass+1)); echo "ok   $2"; else
    fail=$((fail+1)); echo "MISMATCH: $2"; echo " oracle:"; cat "$T/o.txt"; echo " mfl:"; cat "$T/m.txt"; fi
}

# s1 mod-by-(a-b), s2 a/b, s3 guarded (clean), s4 10/(a*a-a), s5 3-param a/(b-c),
# s6 nested-if guard (clean), s7 unary -a+a=0, s8 assign-then-div, s9 chained guard (clean)
printf 'func wrap(a,b){return 100%%(a-b)}\nfunc main(){println(str(wrap(1,2)))}\n' > "$T/s1"; run "$T/s1" "mod by (a-b)"
printf 'func d(a,b){return a/b}\nfunc main(){println(str(d(6,2)))}\n' > "$T/s2"; run "$T/s2" "a/b"
printf 'func g(a,b){if b==0{return 0}return a/b}\nfunc main(){println(str(g(6,2)))}\n' > "$T/s3"; run "$T/s3" "guarded (clean)"
printf 'func k(a){return 10/(a*a-a)}\nfunc main(){println(str(k(3)))}\n' > "$T/s4"; run "$T/s4" "10/(a*a-a)"
printf 'func m(a,b,c){return a/(b-c)}\nfunc main(){println(str(m(1,2,3)))}\n' > "$T/s5"; run "$T/s5" "3-param a/(b-c)"
printf 'func h(a,b){if b!=0{if a>0{return a/b}}return 0}\nfunc main(){println(str(h(1,2)))}\n' > "$T/s6"; run "$T/s6" "nested-if guard (clean)"
printf 'func n(a){return 5/(-a+a)}\nfunc main(){println(str(n(2)))}\n' > "$T/s7"; run "$T/s7" "unary -a+a=0"
printf 'func p(a,b){c:=a-b return 9/c}\nfunc main(){println(str(p(1,2)))}\n' > "$T/s8"; run "$T/s8" "assign then div"
printf 'func q(a,b){if b<1{if b>-1{return 0}}return a/b}\nfunc main(){println(str(q(1,2)))}\n' > "$T/s9"; run "$T/s9" "chained guard (clean)"

echo
echo "self-hosted falsify oracle-diff: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
