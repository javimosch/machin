#!/usr/bin/env bash
# Record/replay gate (Slice 1.1). machin programs are proved data-race-free, so
# the only inter-goroutine nondeterminism is the order channel ops complete. We
# record that order as a sequence of goroutine PATHS (parent-relative ids, stable
# across runs even under concurrent nested spawns) and, on replay, gate each op to
# fire in the recorded order — reproducing the run without recording a single
# memory access. Proves: replay reproduces the recording; a crafted trace controls
# the schedule; nested spawns are deterministic; close is an ordered op.
#
# Compiles each fixture ONCE and drives the binary via MFL_RR_RECORD/REPLAY env,
# so the gate is fast (no recompile per run).
set -u
N="nice -n 15"
M=./bin/machin
T=$(mktemp -d)
pass=0; fail=0
chk() { if [ "$1" = "$2" ]; then pass=$((pass+1)); echo "ok   $3 ($1)"; else fail=$((fail+1)); echo "FAIL $3: got '$1' want '$2'"; fi; }

echo "building machin…"
GOMAXPROCS=4 $N go build -o bin/machin . || { echo "go build failed"; exit 1; }

build() { $N $M build "$1" -o "$2" >/dev/null 2>&1 || { echo "build failed: $1"; exit 1; }; }
rec()   { MFL_RR_RECORD="$2" "$1" 2>/dev/null; }
rep()   { MFL_RR_REPLAY="$2" "$1" 2>/dev/null; }
plain() { "$1" 2>/dev/null; }

# ---- fixture 1: flat 4-goroutine channel race ----
cat > "$T/race.mfl" <<'EOF'
func worker(id, ch) { ch <- id }
func main() {
  ch := make(chan int)
  go worker(1, ch)  go worker(2, ch)  go worker(3, ch)  go worker(4, ch)
  out := ""  i := 0
  for i < 4 { v := <-ch  out = out + str(v)  i = i + 1 }
  println(out)
}
EOF
build "$T/race.mfl" "$T/race"
R=$(rec "$T/race" "$T/tr"); echo "recorded: $R  trace: $(grep '^S' "$T/tr" | tr '\n' ' ')"
U=$(for i in $(seq 1 10); do rep "$T/race" "$T/tr"; done | sort -u); chk "$U" "$R" "10 replays reproduce the recording"
printf 'MFLRR 1\nS 0.1\nS 0\nS 0.2\nS 0\nS 0.3\nS 0\nS 0.4\nS 0\n' > "$T/fwd"; chk "$(rep "$T/race" "$T/fwd")" "1234" "crafted trace 1,2,3,4 -> 1234"
printf 'MFLRR 1\nS 0.4\nS 0\nS 0.3\nS 0\nS 0.2\nS 0\nS 0.1\nS 0\n' > "$T/rev"; chk "$(rep "$T/race" "$T/rev")" "4321" "crafted trace 4,3,2,1 -> 4321"
printf 'MFLRR 1\nS 0.2\nS 0\nS 0.4\nS 0\nS 0.1\nS 0\nS 0.3\nS 0\n' > "$T/int"; chk "$(rep "$T/race" "$T/int")" "2413" "interleaved trace -> 2413"
echo "  plain-run outputs (vary): $(for i in $(seq 1 12); do plain "$T/race"; done | sort -u | tr '\n' ' ')"

# ---- fixture 2: nested concurrent spawns (the gid-race case the path fix targets) ----
cat > "$T/nested.mfl" <<'EOF'
func sub(id, ch) { ch <- id }
func worker(base, ch) { go sub(base*10+1, ch)  go sub(base*10+2, ch) }
func main() {
  ch := make(chan int)
  go worker(1, ch)  go worker(2, ch)
  out := ""  i := 0
  for i < 4 { v := <-ch  out = out + str(v) + "-"  i = i + 1 }
  println(out)
}
EOF
build "$T/nested.mfl" "$T/nested"
R=$(rec "$T/nested" "$T/tn"); echo "nested recorded: $R  trace: $(tr '\n' ' ' < "$T/tn")"
U=$(for i in $(seq 1 10); do rep "$T/nested" "$T/tn"; done | sort -u); chk "$U" "$R" "nested spawns replay deterministically"
echo "  plain nested (vary): $(for i in $(seq 1 10); do plain "$T/nested"; done | sort -u | tr '\n' ' ')"

# ---- fixture 3: close is an ordered op (range-over-channel terminates on it) ----
cat > "$T/closed.mfl" <<'EOF'
func consumer(ch, done) {
  out := ""
  for v := range ch { out = out + str(v) }
  done <- out
}
func main() {
  ch := make(chan int)
  done := make(chan string)
  go consumer(ch, done)
  ch <- 1  ch <- 2  ch <- 3  close(ch)
  r := <-done
  println(r)
}
EOF
build "$T/closed.mfl" "$T/closed"
R=$(rec "$T/closed" "$T/tc"); echo "closed recorded: $R  trace: $(tr '\n' ' ' < "$T/tc")"
U=$(for i in $(seq 1 10); do rep "$T/closed" "$T/tc"; done | sort -u); chk "$U" "$R" "close + range replay deterministically"

# ---- fixture 4: I/O log — time replays the recorded value ----
printf 'func main() { println(str(now_ms())) }\n' > "$T/time.mfl"
build "$T/time.mfl" "$T/time"
R=$(rec "$T/time" "$T/tt"); sleep 1
U=$(for i in $(seq 1 5); do rep "$T/time" "$T/tt"; done | sort -u); chk "$U" "$R" "time replays the recorded value (not the current clock)"
P=$(plain "$T/time"); [ "$P" != "$R" ] && pass=$((pass+1)) && echo "ok   plain time differs from recorded ($P != $R)" || { fail=$((fail+1)); echo "FAIL plain time should differ"; }

# ---- fixture 5: I/O log — stdin replays the recorded input, no real read ----
printf 'func main() { println("got:[" + read_stdin() + "]") }\n' > "$T/stdin.mfl"
build "$T/stdin.mfl" "$T/stdin"
R=$(echo -n "the recorded input" | rec "$T/stdin" "$T/ts")
chk "$(echo -n "SOMETHING ELSE" | rep "$T/stdin" "$T/ts")" "$R" "stdin replays recorded input despite different real stdin"
chk "$(rep "$T/stdin" "$T/ts" < /dev/null)" "$R" "stdin replay does not block on empty real stdin"

# ---- fixture 6: schedule + I/O together (each worker records a timestamp) ----
cat > "$T/combo.mfl" <<'EOF'
func worker(id, ch) { t := now_ms()  ch <- id * 1000000000000 + t }
func main() {
  ch := make(chan int)
  go worker(1, ch)  go worker(2, ch)  go worker(3, ch)
  out := ""  i := 0
  for i < 3 { v := <-ch  out = out + str(v) + " "  i = i + 1 }
  println(out)
}
EOF
build "$T/combo.mfl" "$T/combo"
R=$(rec "$T/combo" "$T/tco"); sleep 1
U=$(for i in $(seq 1 8); do rep "$T/combo" "$T/tco"; done | sort -u); chk "$U" "$R" "schedule + per-worker I/O replay together"

# ---- fixture 7: determinism boundary — honest faithful vs best-effort ----
# a pure program records a `faithful` trace and --verify certifies it.
hdr=$(sed -n 2p "$T/tr"); chk "$hdr" "boundary faithful" "pure program -> faithful trace"
V=$(MFL_RR_REPLAY="$T/tr" MFL_RR_VERIFY=1 "$T/race" 2>&1 >/dev/null); case "$V" in *FAITHFUL*) pass=$((pass+1)); echo "ok   --verify certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL --verify: $V";; esac

# an FFI program records a `best-effort` trace; replay warns; --verify never certifies it.
printf 'extern "m" { header "math.h" link "m" fn sqrt(float) float }\nfunc main() { println(str(sqrt(16.0))) }\n' > "$T/ffi.mfl"
build "$T/ffi.mfl" "$T/ffi"
rec "$T/ffi" "$T/tf" >/dev/null
hdr=$(sed -n 2p "$T/tf"); chk "$hdr" "boundary best-effort" "FFI program -> best-effort trace"
W=$(MFL_RR_REPLAY="$T/tf" "$T/ffi" 2>&1 >/dev/null); case "$W" in *best-effort*) pass=$((pass+1)); echo "ok   replay warns on a best-effort trace";; *) fail=$((fail+1)); echo "FAIL no best-effort warning: $W";; esac
V=$(MFL_RR_REPLAY="$T/tf" MFL_RR_VERIFY=1 "$T/ffi" 2>&1 >/dev/null); case "$V" in *DIVERGED*) pass=$((pass+1)); echo "ok   --verify refuses to certify a best-effort trace (DIVERGED)";; *) fail=$((fail+1)); echo "FAIL best-effort should not certify: $V";; esac

# a select program is now GATED (see fixtures 14/15), so its trace is faithful, not
# best-effort — FFI is the only remaining best-effort boundary.
printf 'func send(ch) { ch <- 7 }\nfunc main() { ch := make(chan int)  go send(ch)  select { case v := <-ch: println(str(v)) } }\n' > "$T/selb.mfl"
build "$T/selb.mfl" "$T/selb"
rec "$T/selb" "$T/tsl" >/dev/null
hdr=$(sed -n 2p "$T/tsl"); chk "$hdr" "boundary faithful" "select program -> faithful trace (gated, not best-effort)"

# ---- fixture 8: `machin replay <trace>` command (program path embedded) ----
R=$($N $M run --record "$T/rc.tr" "$T/race.mfl" 2>/dev/null)   # records with a `program` line
U=$(for i in $(seq 1 5); do $N $M replay "$T/rc.tr" 2>/dev/null; done | sort -u)
chk "$U" "$R" "machin replay <trace> reproduces the run (no source re-specified)"

# ---- fixture 9: a crash replays into a structured causal report ----
# a schedule-caused index-out-of-range; craft the crashing schedule for determinism.
cat > "$T/crash.mfl" <<'EOF'
func worker(id, ch) { ch <- id }
func main() {
  ch := make(chan int)
  go worker(0, ch)  go worker(1, ch)
  xs := []int{100}
  a := <-ch
  println(str(xs[a]))
}
EOF
printf 'MFLRR 1\nboundary faithful\nprogram %s\nsafe 1\nS 0.2\nS 0.1\nS 0\n' "$T/crash.mfl" > "$T/crash.tr"
$N $M replay "$T/crash.tr" >/dev/null 2>&1; rc=$?
[ "$rc" -ne 0 ] && pass=$((pass+1)) && echo "ok   crash trace reproduces the crash (exit $rc)" || { fail=$((fail+1)); echo "FAIL crash should reproduce"; }
J=$($N $M replay "$T/crash.tr" --json 2>&1 >/dev/null)
echo "$J" | grep -q '"panic":"index out of range' && echo "$J" | grep -q '"causalChain":\["0.2","0.1","0"\]' \
  && { pass=$((pass+1)); echo "ok   causal report: $J"; } \
  || { fail=$((fail+1)); echo "FAIL causal report wrong: $J"; }

# ---- fixture 10: concurrent OUTPUT interleaving is captured (no channels) ----
# goroutines that only println (no channel sync) still replay faithfully because
# print statements are gated too.
cat > "$T/prints.mfl" <<'EOF'
func w(id) { println("worker " + str(id) + " done") }
func main() {
  go w(1)  go w(2)  go w(3)  go w(4)  go w(5)
  sleep(150)
  println("main done")
}
EOF
build "$T/prints.mfl" "$T/prints"
R=$(rec "$T/prints" "$T/tp")
U=$(for i in $(seq 1 8); do rep "$T/prints" "$T/tp"; done | md5sum | cut -d' ' -f1)
RH=$(echo "$R" | md5sum | cut -d' ' -f1)
# every replay must produce the identical multi-line output.
allsame=$(for i in $(seq 1 8); do rep "$T/prints" "$T/tp" | md5sum; done | sort -u | wc -l)
chk "$allsame" "1" "concurrent println interleaving replays identically"
Vp=$(MFL_RR_REPLAY="$T/tp" MFL_RR_VERIFY=1 "$T/prints" 2>&1 >/dev/null); case "$Vp" in *FAITHFUL*) pass=$((pass+1)); echo "ok   concurrent-print replay certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL: $Vp";; esac
echo "  plain concurrent-print orderings (vary): $(for i in $(seq 1 8); do plain "$T/prints" | md5sum | cut -c1-6; done | sort -u | tr '\n' ' ')"

# ---- fixture 11: real corpus concurrent programs replay faithfully ----
for cf in examples/complex/channels.mfl examples/complex/goroutines.mfl; do
  [ -f "$cf" ] || continue
  build "$cf" "$T/corp"
  rec "$T/corp" "$T/tcorp" >/dev/null
  Vc=$(MFL_RR_REPLAY="$T/tcorp" MFL_RR_VERIFY=1 "$T/corp" 2>&1 >/dev/null)
  case "$Vc" in *FAITHFUL*) pass=$((pass+1)); echo "ok   corpus $(basename "$cf") replays FAITHFUL";; *) fail=$((fail+1)); echo "FAIL corpus $(basename "$cf"): $Vc";; esac
done

# ---- fixture 12: rand_bytes is I/O — recording makes crypto FAITHFULLY replayable ----
# rand draws fresh entropy each run (plain runs vary), but replay reproduces the recorded
# draw byte-for-byte and --verify certifies FAITHFUL (not best-effort).
cat > "$T/rand.mfl" <<'EOF'
func main() {
  b := rand_bytes(16)
  s := 0  i := 0
  for i < len(b) { s = s + int(byte_at(b, i))  i = i + 1 }
  println(str(s))
}
EOF
build "$T/rand.mfl" "$T/rand"
Rr=$(rec "$T/rand" "$T/trand")
allsame=$(for i in $(seq 1 8); do rep "$T/rand" "$T/trand"; done | sort -u | wc -l)
chk "$allsame" "1" "rand_bytes replays identically"
chk "$(rep "$T/rand" "$T/trand")" "$Rr" "rand replay reproduces the recorded draw"
Vr=$(MFL_RR_REPLAY="$T/trand" MFL_RR_VERIFY=1 "$T/rand" 2>&1 >/dev/null); case "$Vr" in *FAITHFUL*) pass=$((pass+1)); echo "ok   rand_bytes replay certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL rand verify: $Vr";; esac
echo "  plain rand outputs (vary): $(for i in $(seq 1 6); do plain "$T/rand"; done | sort -u | tr '\n' ' ')"

# ---- fixture 13: file reads are recorded — the "mailable crash" (replay w/o the files) ----
# record with the input file present, then DELETE it; replay must still reproduce from the
# recorded bytes (a plain run now reads empty), proving the trace is self-contained.
echo "payload-13-$$" > "$T/in.txt"
cat > "$T/file.mfl" <<EOF
func main() { println("read:" + read_file("$T/in.txt")) }
EOF
build "$T/file.mfl" "$T/file"
Rf=$(rec "$T/file" "$T/tfile")
rm -f "$T/in.txt"
chk "$(rep "$T/file" "$T/tfile")" "$Rf" "file replay reproduces recorded bytes after the file is deleted"
chk "$(plain "$T/file")" "read:" "plain run reads empty once the file is gone (control)"
Vf=$(MFL_RR_REPLAY="$T/tfile" MFL_RR_VERIFY=1 "$T/file" 2>&1 >/dev/null); case "$Vf" in *FAITHFUL*) pass=$((pass+1)); echo "ok   deleted-file replay certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL file verify: $Vf";; esac

# ---- fixture 14: select recv is GATED — a select-using program replays FAITHFULLY ----
# two feeders race into a select (plain runs vary in a/b interleaving), but replay
# reproduces the recorded choice sequence and --verify certifies FAITHFUL. The chosen
# case index is recorded + forced on replay, so select is no longer best-effort.
cat > "$T/sel.mfl" <<'EOF'
func feed(ch, base, dly) { i := 0  for i < 5 { sleep(dly)  ch <- base + i  i = i + 1 } }
func main() {
  a := make(chan int)  b := make(chan int)
  go feed(a, 100, 2)  go feed(b, 200, 3)
  got := 0  order := ""
  for got < 10 {
    select {
    case x := <-a: order = order + "a" + str(x) + " "  got = got + 1
    case y := <-b: order = order + "b" + str(y) + " "  got = got + 1
    }
  }
  println(order)
}
EOF
build "$T/sel.mfl" "$T/sel"
Rs=$(rec "$T/sel" "$T/tsel")
allsame=$(for i in $(seq 1 8); do rep "$T/sel" "$T/tsel"; done | sort -u | wc -l)
chk "$allsame" "1" "select recv replays identically"
chk "$(rep "$T/sel" "$T/tsel")" "$Rs" "select replay reproduces the recorded interleaving"
chk "$(grep '^boundary' "$T/tsel")" "boundary faithful" "select trace boundary is faithful (not best-effort)"
Vs=$(MFL_RR_REPLAY="$T/tsel" MFL_RR_VERIFY=1 "$T/sel" 2>&1 >/dev/null); case "$Vs" in *FAITHFUL*) pass=$((pass+1)); echo "ok   select replay certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL select verify: $Vs";; esac
echo "  plain select orderings (distinct): $(for i in $(seq 1 8); do plain "$T/sel"; done | sort -u | wc -l)"

# ---- fixture 15: select DEFAULT firing is gated — exact miss count reproduces ----
# how many times default fires depends on timing (plain runs vary); replay reproduces
# the recorded count exactly.
cat > "$T/seld.mfl" <<'EOF'
func feed(ch) { i := 0  for i < 5 { sleep(2)  ch <- i  i = i + 1 } }
func main() {
  a := make(chan int)
  go feed(a)
  got := 0  misses := 0
  for got < 5 {
    select { case x := <-a: got = got + 1 + x - x  default: misses = misses + 1 }
    sleep(1)
  }
  println("misses:" + str(misses))
}
EOF
build "$T/seld.mfl" "$T/seld"
Rd=$(rec "$T/seld" "$T/tseld")
allsame=$(for i in $(seq 1 8); do rep "$T/seld" "$T/tseld"; done | sort -u | wc -l)
chk "$allsame" "1" "select-default miss count replays identically"
chk "$(rep "$T/seld" "$T/tseld")" "$Rd" "select-default replay reproduces the recorded miss count"

# ---- fixture 16: raw fd socket I/O is captured — self-contained replay (no network) ----
# a loopback server+client exchange whose message carries now_ms()%1000 (varies each run);
# replay reproduces it with NO real sockets (listen/accept/dial/read/write all served from
# the I/O log), even while another process holds the port.
cat > "$T/sock.mfl" <<'EOF'
func serve(l) { fd := accept(l)  msg := read(fd)  n := write(fd, "echo:" + msg)  n = n + 0  close(fd) }
func main() {
  l := listen(18197)
  go serve(l)
  sleep(50)
  c := dial("127.0.0.1", 18197)
  w := write(c, "hi-" + str(now_ms() % 1000))  w = w + 0
  resp := read(c)
  println(resp)
  close(c)
}
EOF
build "$T/sock.mfl" "$T/sock"
Rk=$(rec "$T/sock" "$T/tsock")
chk "$(grep '^boundary' "$T/tsock")" "boundary faithful" "raw-socket trace boundary is faithful"
allsame=$(for i in $(seq 1 5); do rep "$T/sock" "$T/tsock"; done | sort -u | wc -l)
chk "$allsame" "1" "socket exchange replays identically (no network)"
chk "$(rep "$T/sock" "$T/tsock")" "$Rk" "socket replay reproduces the recorded exchange"
Vk=$(MFL_RR_REPLAY="$T/tsock" MFL_RR_VERIFY=1 "$T/sock" 2>&1 >/dev/null); case "$Vk" in *FAITHFUL*) pass=$((pass+1)); echo "ok   socket replay certifies FAITHFUL";; *) fail=$((fail+1)); echo "FAIL socket verify: $Vk";; esac

# ---- fixture 17: high-level HTTP is honestly flagged best-effort (response not yet captured) ----
# (points at a fast-refusing local port; the boundary is written at init, no external network.)
printf 'func main() { s, b, e := http_get("http://127.0.0.1:1")  b=b  e=e  println(str(s)) }\n' > "$T/http.mfl"
build "$T/http.mfl" "$T/http"
rec "$T/http" "$T/thttp" >/dev/null 2>&1
chk "$(grep '^boundary' "$T/thttp")" "boundary best-effort" "http_get trace is honestly best-effort (uncaptured)"

echo
echo "record/replay gate: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
