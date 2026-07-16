#!/usr/bin/env bash
# Behavioral regression gate for the Falsifier (falsify.go).
#
# Unlike verify-race.sh (an oracle diff against a self-hosted pass), the falsifier
# is not yet self-hosted, so this drives the real `machin` binary end to end:
#   - each planted bug class must be FOUND (the right FALS code) AND its emitted
#     repro must PANIC under --safe (proving the counterexample is a real bug);
#   - adversarial correct-but-tricky code must stay CLEAN (no false positives);
#   - a real-corpus sweep must not crash, and every `unknown` is bucketed and
#     printed (no silent under-reporting).
set -u
N="nice -n 15"
MACHIN=./bin/machin
T=$(mktemp -d)
pass=0
fail=0

echo "building machin…"
GOMAXPROCS=4 $N go build -o bin/machin . || { echo "go build failed"; exit 1; }

jhas() { # out.json target code  ->  "True"/"False": a finding {decl,code} exists
  python3 -c "import json;d=json.load(open('$T/out.json'));print(any(f['decl']=='$1' and f['code']=='$2' for f in d['findings']))"
}
jclean() { # out.json target  ->  "True": no finding for target
  python3 -c "import json;d=json.load(open('$T/out.json'));print(not any(f['decl']=='$1' for f in d['findings']))"
}

# expect_bug FILE TARGET CODE LABEL — finding present AND repro panics under --safe.
expect_bug() {
  local file=$1 target=$2 code=$3 label=$4
  $N "$MACHIN" falsify --json "$file" > "$T/out.json" 2>/dev/null
  if [ "$(jhas "$target" "$code")" != "True" ]; then
    echo "FAIL (no $code on $target): $label"; fail=$((fail+1)); return
  fi
  rm -rf "$T/rep"; $N "$MACHIN" falsify --repro "$T/rep" "$file" >/dev/null 2>&1
  local r; r=$(ls "$T/rep"/repro_"${target}"_*.mfl 2>/dev/null | head -1)
  if [ -z "$r" ]; then echo "FAIL (no repro): $label"; fail=$((fail+1)); return; fi
  local out; out=$($N "$MACHIN" run --safe "$r" 2>&1); local rc=$?
  if [ $rc -eq 0 ]; then echo "FAIL (repro did not panic): $label"; echo "$out"; fail=$((fail+1)); return; fi
  case "$out" in
    *"out of range"*|*"by zero"*) pass=$((pass+1)); echo "ok  $label" ;;
    *) echo "FAIL (repro wrong trap): $label — $out"; fail=$((fail+1)) ;;
  esac
}

# expect_clean FILE TARGET LABEL — no false positive on correct-but-tricky code.
expect_clean() {
  local file=$1 target=$2 label=$3
  $N "$MACHIN" falsify --json "$file" > "$T/out.json" 2>/dev/null
  if [ "$(jclean "$target")" = "True" ]; then pass=$((pass+1)); echo "ok  $label"
  else echo "FAIL (false positive): $label"; python3 -c "import json;print(json.load(open('$T/out.json'))['findings'])"; fail=$((fail+1)); fi
}

echo; echo "== planted bugs (must be found + repro must panic) =="

cat > "$T/b1.mfl" <<'EOF'
func sumbad(xs){total:=0 i:=0 for i<=len(xs){total=total+xs[i] i=i+1}return total}
func main(){println(str(sumbad([]int{1,2})))}
EOF
expect_bug "$T/b1.mfl" sumbad FALS001 "off-by-one index"

cat > "$T/b2.mfl" <<'EOF'
func avg(xs){total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}
func main(){println(str(avg([]int{1,2})))}
EOF
expect_bug "$T/b2.mfl" avg FALS002 "empty-average div-by-zero"

cat > "$T/b3.mfl" <<'EOF'
func wrap(a,b){return 100%(a-b)}
func main(){println(str(wrap(1,2)))}
EOF
expect_bug "$T/b3.mfl" wrap FALS002 "mod by (a-b)"

cat > "$T/b4.mfl" <<'EOF'
func helper(n){return 100/n}
func caller(n){return helper(n)+1}
func main(){println(str(caller(2)))}
EOF
expect_bug "$T/b4.mfl" caller FALS002 "interprocedural div-by-zero"

cat > "$T/b5.mfl" <<'EOF'
type Cfg struct{n int}
func run(c){return 100/c.n}
func main(){println(str(run(Cfg{n:1})))}
EOF
expect_bug "$T/b5.mfl" run FALS002 "div by struct field"

cat > "$T/b6.mfl" <<'EOF'
type Box struct{sz int}
func at(b,xs){return xs[b.sz]}
func main(){println(str(at(Box{sz:0},[]int{1})))}
EOF
expect_bug "$T/b6.mfl" at FALS001 "index by struct field"

echo; echo "== adversarial correct-but-tricky code (must stay clean) =="

cat > "$T/c1.mfl" <<'EOF'
func safeavg(xs){if len(xs)==0{return 0}total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}
func main(){println(str(safeavg([]int{1,2})))}
EOF
expect_clean "$T/c1.mfl" safeavg "guarded average"

cat > "$T/c2.mfl" <<'EOF'
func sumok(xs){total:=0 for _,v:=range xs{total=total+v}return total}
func main(){println(str(sumok([]int{1,2})))}
EOF
expect_clean "$T/c2.mfl" sumok "len-bounded sum"

cat > "$T/c3.mfl" <<'EOF'
func ratio(a,b){if b==0{return 0}return a/b}
func main(){println(str(ratio(6,2)))}
EOF
expect_clean "$T/c3.mfl" ratio "guarded scalar division"

cat > "$T/c4.mfl" <<'EOF'
type P struct{n int}
func vs(c){d:=c d.n=d.n+1 return d.n}
func main(){println(str(vs(P{n:1})))}
EOF
expect_clean "$T/c4.mfl" vs "struct copy value-semantics"

cat > "$T/c5.mfl" <<'EOF'
func at(xs,i){if i<0{return 0}if i>=len(xs){return 0}return xs[i]}
func main(){println(str(at([]int{1,2},5)))}
EOF
expect_clean "$T/c5.mfl" at "fully guarded index"

cat > "$T/c6.mfl" <<'EOF'
func fact(n){if n<=1{return 1}return n*fact(n-1)}
func main(){println(str(fact(3)))}
EOF
expect_clean "$T/c6.mfl" fact "recursion (inconclusive, not FP)"

echo; echo "== real-corpus sweep (must not crash; unknowns bucketed) =="
# multi_return.mfl carries genuine latent bugs — assert the falsifier catches them.
if [ -f examples/complex/multi_return.mfl ]; then
  $N "$MACHIN" falsify --json examples/complex/multi_return.mfl > "$T/out.json" 2>/dev/null
  if [ "$(jhas divmod FALS002)" = "True" ] && [ "$(jhas minmax FALS001)" = "True" ]; then
    pass=$((pass+1)); echo "ok  corpus: divmod+minmax latent bugs found"
  else
    fail=$((fail+1)); echo "FAIL corpus: expected divmod FALS002 + minmax FALS001"
  fi
fi
for f in examples/complex/ranges.mfl examples/complex/json_api.mfl examples/hello.mfl; do
  [ -f "$f" ] || continue
  if $N "$MACHIN" falsify --json "$f" > "$T/out.json" 2>/dev/null; then
    python3 -c "import json;d=json.load(open('$T/out.json'));c=d['coverage'];print('    bucket %-40s checked=%d skipped=%d unknown=%d cex=%d'%('$f',c['checked'],c['skipped'],c['allUnknown'],d['counterexamples']))"
    pass=$((pass+1))
  else
    echo "FAIL corpus sweep crashed on $f"; fail=$((fail+1))
  fi
done

echo
echo "falsify verify: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
