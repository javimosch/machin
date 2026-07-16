#!/usr/bin/env bash
# Phase 0 spike gate for sound record/replay.
#
# machin programs are proved data-race-free, so the ONLY inter-goroutine
# nondeterminism is the order channel operations complete. We record that order
# as a sequence of goroutine ids and, on replay, gate each channel op to fire in
# the recorded order — reproducing the run without recording a single memory
# access. This gate proves: (1) replay reproduces the recorded execution
# deterministically, and (2) the trace fully CONTROLS the schedule (a crafted
# trace yields exactly its corresponding output).
set -u
N="nice -n 15"
M=./bin/machin
T=$(mktemp -d)
pass=0; fail=0
chk() { if [ "$1" = "$2" ]; then pass=$((pass+1)); echo "ok   $3 ($1)"; else fail=$((fail+1)); echo "FAIL $3: got '$1' want '$2'"; fi; }

echo "building machin…"
GOMAXPROCS=4 $N go build -o bin/machin . || { echo "go build failed"; exit 1; }

cat > "$T/race.mfl" <<'EOF'
func worker(id, ch) { ch <- id }
func main() {
  ch := make(chan int)
  go worker(1, ch)
  go worker(2, ch)
  go worker(3, ch)
  go worker(4, ch)
  out := ""
  i := 0
  for i < 4 { v := <-ch  out = out + str(v)  i = i + 1 }
  println(out)
}
EOF

# 1. record, then replay 10x — every replay must reproduce the recorded output.
REC=$($N $M run --record "$T/trace.txt" "$T/race.mfl" 2>/dev/null)
echo "recorded output: $REC   trace: $(tr '\n' ' ' < "$T/trace.txt")"
uniq_replays=$(for i in $(seq 1 10); do $N $M run --replay "$T/trace.txt" "$T/race.mfl" 2>/dev/null; done | sort -u)
chk "$uniq_replays" "$REC" "10 replays reproduce the recorded run"

# 2. crafted traces fully control the schedule.
printf '1\n2\n3\n4\n0\n0\n0\n0\n' > "$T/fwd"; chk "$($N $M run --replay "$T/fwd" "$T/race.mfl" 2>/dev/null)" "1234" "trace 1,2,3,4 -> 1234"
printf '4\n3\n2\n1\n0\n0\n0\n0\n' > "$T/rev"; chk "$($N $M run --replay "$T/rev" "$T/race.mfl" 2>/dev/null)" "4321" "trace 4,3,2,1 -> 4321"
printf '2\n0\n4\n0\n1\n0\n3\n0\n' > "$T/int"; chk "$($N $M run --replay "$T/int" "$T/race.mfl" 2>/dev/null)" "2413" "interleaved trace -> 2413"

# 3. (informational) plain runs vary — the nondeterminism replay pins down.
echo "plain-run outputs (varies by scheduler): $(for i in $(seq 1 12); do $N $M run "$T/race.mfl" 2>/dev/null; done | sort -u | tr '\n' ' ')"

echo
echo "record/replay spike: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
