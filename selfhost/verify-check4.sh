#!/usr/bin/env bash
# Stage-3 sub-slice (4) verifier: the builtin signature table -> FULL CORPUS parity.
# Diffs `machin checktest --program` against the MFL checker over every encodable
# program in ~/ai + framework, including the checker checking its OWN source.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
export GOMAXPROCS="${GOMAXPROCS:-4}"      # keep the build off all cores
N="nice -n 15"

echo "building Go machin (oracle) + MFL checker…"
$N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/checkmain.src > /tmp/sh-checker.mfl
$N "$MACHIN" build /tmp/sh-checker.mfl -o selfhost/mfl-check

T=$(mktemp -d); pass=0; fail=0; skip=0

run() { # <mfl-file>
  local m o
  m=$(timeout 40 $N ./selfhost/mfl-check --program "$1" 2>/dev/null)
  [ "$m" = "(unsupported)" ] && { skip=$((skip+1)); return; }
  o=$($N "$MACHIN" checktest --program "$1" 2>/dev/null)
  if [ "$o" = "$m" ]; then pass=$((pass+1)); else
    fail=$((fail+1)); echo "MISMATCH: $1"; diff <(echo "$o") <(echo "$m") | head -8
  fi
}

# hand battery: a broad builtin sweep + all four multi-return builtins
cat > "$T/h1.src" <<'EOF'
func main() {
    xs := []int{3, 1, 2}
    xs = append(xs, 5)
    n := len(xs)
    s := "Hello, World"
    parts := split(s, ", ")
    joined := join(parts, "-")
    m := make(map[string]int)
    m["k"] = 42
    ok := has(m, "k")
    ks := keys(m)
    f := sqrt(2.0)
    g := float(n) * f
    b := sha256_bytes(bytes(s))
    hx := to_hex(b)
    println(to_upper(joined) + " " + str(n) + " " + hx)
    files := list_dir(".")
}
EOF
cat > "$T/h2.src" <<'EOF'
func main() {
    code, out, err := exec("ls")
    status, body, e2 := http_get("http://x")
    v, e3 := json_get(body, "field")
    write(1, out + body + v + str(code + status))
}
EOF
$N "$MACHIN" encode "$T/h1.src" > "$T/h1.mfl" 2>/dev/null; run "$T/h1.mfl"
$N "$MACHIN" encode "$T/h2.src" > "$T/h2.mfl" 2>/dev/null; run "$T/h2.mfl"

# self-application: the checker checks its own composed source
cp /tmp/sh-checker.mfl "$T/self.mfl"; run "$T/self.mfl"

# full corpus: every encodable program in ~/ai + framework
i=0
for f in $(find "$HOME/ai" -maxdepth 4 -name '*.src' 2>/dev/null | sort -u; ls framework/*.src 2>/dev/null); do
  out="$T/c$i.mfl"; $N "$MACHIN" encode "$f" > "$out" 2>/dev/null
  [ -s "$out" ] && run "$out"; i=$((i+1))
done

rm -rf "$T"
echo "----"
echo "PASS $pass  FAIL $fail  (SKIP $skip — select / closures)"
[ "$fail" -eq 0 ]
