#!/usr/bin/env bash
# Oracle-diff gate for the SELF-HOSTED falsify pass (selfhost/falsify.src): its
# findings must be byte-identical to the Go reference `machin falsifytest --program`.
#
# (Distinct from the repo-root verify-falsify.sh, which behaviorally tests the Go
# pass. This one proves the MFL port reproduces the Go oracle exactly.)
#
# Phase 2 slices 2.1-2.5 + Phase 3: int/[]int/float/string/bool/struct params;
# arithmetic, comparisons, if/while/for-range (incl. string range), index
# (FALS001), len, abs, field access/assign, interprocedural inlining, div/mod-by-
# zero (FALS002), AND declarative contracts requires/ensures (FALS010). Full
# parity with the Go pass.
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

# []int fixtures (the real Phase 1 patterns): index-OOB, len, while, for-range.
printf 'func sumbad(xs){total:=0 i:=0 for i<=len(xs){total=total+xs[i] i=i+1}return total}\nfunc main(){println(str(sumbad([]int{1,2})))}\n' > "$T/v1"; run "$T/v1" "sumbad off-by-one (FALS001)"
printf 'func avg(xs){total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}\nfunc main(){println(str(avg([]int{1,2})))}\n' > "$T/v2"; run "$T/v2" "avg empty div (FALS002)"
printf 'func safeavg(xs){if len(xs)==0{return 0}total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}\nfunc main(){println(str(safeavg([]int{1,2})))}\n' > "$T/v3"; run "$T/v3" "safeavg guarded (clean)"
printf 'func sumok(xs){total:=0 for _,v:=range xs{total=total+v}return total}\nfunc main(){println(str(sumok([]int{1,2})))}\n' > "$T/v4"; run "$T/v4" "sumok (clean)"
printf 'func firstgap(xs){return xs[len(xs)-5]}\nfunc main(){println(str(firstgap([]int{1,2})))}\n' > "$T/v5"; run "$T/v5" "firstgap neg index (FALS001)"
printf 'func at(xs,i){if i<0{return 0}if i>=len(xs){return 0}return xs[i]}\nfunc main(){println(str(at([]int{1,2},5)))}\n' > "$T/v6"; run "$T/v6" "guarded index (clean)"
printf 'func first(xs){return xs[0]}\nfunc main(){println(str(first([]int{9})))}\n' > "$T/v7"; run "$T/v7" "xs[0] on empty (FALS001)"
[ -f examples/complex/multi_return.mfl ] && run examples/complex/multi_return.mfl "corpus multi_return (minmax+divmod)"

# float + string fixtures.
printf 'func fdiv(a){return 1.0/a}\nfunc main(){println(str(fdiv(2.0)))}\n' > "$T/f1"; run "$T/f1" "float 1.0/a div (FALS002)"
printf 'func fmath(a,b){c:=a+b-a*b if c<b||c>a{return c}return a}\nfunc main(){println(str(fmath(1.0,2.0)))}\n' > "$T/f2"; run "$T/f2" "float arith+cmp (clean)"
printf 'func mix(a,x){return x/float(a)}\nfunc main(){println(str(mix(2,3.0)))}\n' > "$T/f3"; run "$T/f3" "mixed int/float div"
printf 'func fg(a){if a==0.0{return 0.0}return 1.0/a}\nfunc main(){println(str(fg(2.0)))}\n' > "$T/f4"; run "$T/f4" "float guarded (clean)"
printf 'func scat(s,t){if s==t{return len(s)}if s<t{return 0}if s>t{return 1}return len(s+t)}\nfunc main(){println(str(scat("a","b")))}\n' > "$T/f5"; run "$T/f5" "string concat+cmp (clean)"
printf 'func slen(s){return 10/len(s)}\nfunc main(){println(str(slen("hi")))}\n' > "$T/f6"; run "$T/f6" "10/len(s) empty-string div (FALS002)"
printf 'func vowels(s){n:=0 for _,c:=range s{if c=="a"{n=n+1}}return 100/n}\nfunc main(){println(str(vowels("aaa")))}\n' > "$T/f7"; run "$T/f7" "string range + div"
printf 'func firstc(s){return s[0]}\nfunc main(){println(firstc("x"))}\n' > "$T/f8"; run "$T/f8" "string index s[0] (FALS001)"

# struct + bool fixtures.
printf 'type Cfg struct{n int}\nfunc run(c){return 100/c.n}\nfunc main(){println(str(run(Cfg{n:1})))}\n' > "$T/t1"; run "$T/t1" "div by struct field (FALS002)"
printf 'type Box struct{sz int}\nfunc at(b,xs){return xs[b.sz]}\nfunc main(){println(str(at(Box{sz:0},[]int{1})))}\n' > "$T/t2"; run "$T/t2" "index by struct field (FALS001)"
printf 'type P struct{n int}\nfunc vs(c){d:=c d.n=d.n+1 return d.n}\nfunc main(){println(str(vs(P{n:1})))}\n' > "$T/t3"; run "$T/t3" "struct copy value-semantics (clean)"
printf 'type M struct{a int b bool}\nfunc g(m){if m.b{return 0}return 10/m.a}\nfunc main(){println(str(g(M{a:1,b:true})))}\n' > "$T/t4"; run "$T/t4" "struct with bool field"
printf 'func bs(flag,a){if flag{return 0}return 10/a}\nfunc main(){println(str(bs(true,2)))}\n' > "$T/t5"; run "$T/t5" "bool param (true/false render)"
printf 'type S struct{name string cnt int}\nfunc h(s){return len(s.name)/s.cnt}\nfunc main(){println(str(h(S{name:"x",cnt:1})))}\n' > "$T/t6"; run "$T/t6" "struct string+int fields"

# interprocedural inlining fixtures.
printf 'func helper(n){return 100/n}\nfunc caller(n){return helper(n)+1}\nfunc main(){println(str(caller(2)))}\n' > "$T/i1"; run "$T/i1" "interproc div-by-zero"
printf 'func dbl(y){return y*2}\nfunc use(x){return x+dbl(x)}\nfunc main(){println(str(use(3)))}\n' > "$T/i2"; run "$T/i2" "interproc clean"
printf 'func fact(n){if n<=1{return 1}return n*fact(n-1)}\nfunc main(){println(str(fact(3)))}\n' > "$T/i3"; run "$T/i3" "recursion (depth guard)"
printf 'func idx(xs,i){return xs[i]}\nfunc get(xs){return idx(xs,5)}\nfunc main(){println(str(get([]int{1,2})))}\n' > "$T/i4"; run "$T/i4" "interproc index-OOB (FALS001)"
printf 'func mag(a){return abs(a)}\nfunc r(a){return 10/(mag(a)-1.0)}\nfunc main(){println(str(r(2)))}\n' > "$T/i5"; run "$T/i5" "abs + interproc float"
printf 'type Cfg struct{n int}\nfunc div(c){return 100/c.n}\nfunc run(c){return div(c)}\nfunc main(){println(str(run(Cfg{n:1})))}\n' > "$T/i6"; run "$T/i6" "interproc struct-field div"
printf 'func chain3(n){return n}\nfunc chain2(n){return chain3(n)+1}\nfunc chain1(n){return 9/chain2(n)}\nfunc main(){println(str(chain1(1)))}\n' > "$T/i7"; run "$T/i7" "3-deep call chain"

# Phase 3: declarative contracts (requires/ensures, FALS010).
printf 'func div(a,b) requires b != 0 { return a/b }\nfunc main(){println(str(div(6,2)))}\n' > "$T/p1"; run "$T/p1" "requires suppresses div-by-zero"
printf 'func bad(x) (r) ensures r >= x { return x - 1 }\nfunc main(){println(str(bad(5)))}\n' > "$T/p2"; run "$T/p2" "ensures violated (FALS010)"
printf 'func myabs(x) (r) ensures r >= 0 { if x < 0 { return 0 - x } return x }\nfunc main(){println(str(myabs(-3)))}\n' > "$T/p3"; run "$T/p3" "ensures holds (clean)"
printf 'func recip(x) (r) requires x > 0  ensures r > 0 { return 10/x - 5 }\nfunc main(){println(str(recip(1)))}\n' > "$T/p4"; run "$T/p4" "requires narrows, FALS010 remains"
printf 'func between(x) (r) requires x > 0  requires x < 3  ensures r == x { return x }\nfunc main(){println(str(between(1)))}\n' > "$T/p5"; run "$T/p5" "two requires (clean)"
printf 'func clamp(x,lo,hi) (r) requires lo <= hi  ensures r >= lo  ensures r <= hi { r = x  if r < lo { r = lo }  if r > hi { r = hi }  return r }\nfunc main(){println(str(clamp(5,0,10)))}\n' > "$T/p6"; run "$T/p6" "clamp 2-ensures (clean)"
printf 'func clampbad(x,lo,hi) (r) requires lo <= hi  ensures r >= lo  ensures r <= hi { return x }\nfunc main(){println(str(clampbad(5,0,10)))}\n' > "$T/p7"; run "$T/p7" "clamp-bad (FALS010)"

echo
echo "self-hosted falsify oracle-diff: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
