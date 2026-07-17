#!/usr/bin/env bash
# Runtime deadlock detector gate. machin proves programs data-race-free, and its channels
# are unbounded (send never blocks), so the only blocking op is a receive on an empty open
# channel. A deadlock is every live goroutine parked in such a receive with no one left to
# send — the runtime detects it and exits 2 with a message, instead of hanging forever.
# Proves: real deadlocks are caught; correct concurrent programs are NOT flagged.
set -u
N="nice -n 15"
M=./bin/machin
T=$(mktemp -d)
pass=0; fail=0

echo "building machin…"
GOMAXPROCS=4 $N go build -o bin/machin . || { echo "go build failed"; exit 1; }

build() { $N $M build "$1" -o "$2" >/dev/null 2>&1 || { echo "build failed: $1"; exit 1; }; }
# expect_deadlock <bin> <label>: must exit 2 and print "deadlock"
expect_deadlock() {
  out=$(timeout 15 "$1" 2>&1); rc=$?
  if [ "$rc" -eq 2 ] && echo "$out" | grep -qi deadlock; then
    pass=$((pass+1)); echo "ok   $2 -> deadlock detected (exit 2)"
  else
    fail=$((fail+1)); echo "FAIL $2: rc=$rc out='$out'"
  fi
}
# expect_ok <bin> <label> <want-substr>: must exit 0 (no false deadlock)
expect_ok() {
  out=$(timeout 15 "$1" 2>&1); rc=$?
  if [ "$rc" -eq 0 ] && echo "$out" | grep -q "$3"; then
    pass=$((pass+1)); echo "ok   $2 -> completed, no false deadlock"
  else
    fail=$((fail+1)); echo "FAIL $2: rc=$rc out='$out'"
  fi
}

# --- deadlocks that must be caught ---
cat > "$T/dl_main.mfl" <<'EOF'
func main() { ch := make(chan int)  v := <-ch  println(str(v)) }
EOF
build "$T/dl_main.mfl" "$T/dl_main"; expect_deadlock "$T/dl_main" "main recv, nobody sends"

cat > "$T/dl_mutual.mfl" <<'EOF'
func a(x, y) { v := <-x  y <- v }
func main() { p := make(chan int)  q := make(chan int)  go a(p, q)  r := <-q  p <- r  println("done") }
EOF
build "$T/dl_mutual.mfl" "$T/dl_mutual"; expect_deadlock "$T/dl_mutual" "mutual wait (main + goroutine)"

cat > "$T/dl_finished.mfl" <<'EOF'
func worker(done) { }
func main() { done := make(chan int)  go worker(done)  v := <-done  println(str(v)) }
EOF
build "$T/dl_finished.mfl" "$T/dl_finished"; expect_deadlock "$T/dl_finished" "await a goroutine that never sends"

# --- correct programs that must NOT be flagged ---
cat > "$T/ok_pc.mfl" <<'EOF'
func prod(ch) { i := 0  for i < 3 { ch <- i  i = i + 1 }  close(ch) }
func main() { ch := make(chan int)  go prod(ch)  sum := 0  for v := range ch { sum = sum + v }  println("sum:" + str(sum)) }
EOF
build "$T/ok_pc.mfl" "$T/ok_pc"; expect_ok "$T/ok_pc" "producer/consumer" "sum:3"

cat > "$T/ok_pool.mfl" <<'EOF'
func worker(jobs, results) { for j := range jobs { results <- j * j } }
func feed(jobs) { i := 0  for i < 20 { jobs <- i  i = i + 1 }  close(jobs) }
func main() {
  jobs := make(chan int)  results := make(chan int)
  go worker(jobs, results)  go worker(jobs, results)  go worker(jobs, results)  go feed(jobs)
  sum := 0  n := 0
  for n < 20 { sum = sum + <-results  n = n + 1 }
  println("sum:" + str(sum))
}
EOF
build "$T/ok_pool.mfl" "$T/ok_pool"
# run the worker pool several times — a false positive under scheduling jitter must never appear
poolok=1; for i in $(seq 1 6); do o=$(timeout 15 "$T/ok_pool" 2>&1); [ "$?" -eq 0 ] && echo "$o" | grep -q "sum:2470" || poolok=0; done
[ "$poolok" -eq 1 ] && { pass=$((pass+1)); echo "ok   worker pool x6 -> never a false deadlock"; } || { fail=$((fail+1)); echo "FAIL worker pool flaked"; }

for cf in examples/complex/channels.mfl examples/complex/goroutines.mfl; do
  [ -f "$cf" ] || continue
  build "$cf" "$T/corp"
  o=$(timeout 15 "$T/corp" 2>&1); rc=$?
  [ "$rc" -eq 0 ] && { pass=$((pass+1)); echo "ok   corpus $(basename "$cf") -> no false deadlock"; } || { fail=$((fail+1)); echo "FAIL corpus $(basename "$cf"): rc=$rc"; }
done

echo
echo "deadlock gate: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
