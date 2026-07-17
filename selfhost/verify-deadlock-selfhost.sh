#!/usr/bin/env bash
# Oracle-diff gate for the SELF-HOSTED deadlock finder (selfhost/deadlock.src): its DL001
# findings must be byte-identical to the Go reference `machin deadlocktest --program`.
# (Distinct from the repo-root verify-deadlock.sh, which behaviorally tests the runtime
# detector. This one proves the MFL port reproduces the Go compile-time oracle exactly.)
set -u
N="nice -n 15"
MACHIN=./bin/machin
T=$(mktemp -d)
pass=0; fail=0

echo "building Go machin (oracle) + MFL deadlock pass…"
GOMAXPROCS=4 $N go build -trimpath -o bin/machin . || { echo "go build failed"; exit 1; }
$N "$MACHIN" encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
    selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src \
    selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/compile.src \
    selfhost/deadlock.src selfhost/deadlockmain.src > "$T/sh.mfl" || { echo "encode failed"; exit 1; }
$N "$MACHIN" build "$T/sh.mfl" -o selfhost/mfl-deadlock || { echo "build failed"; exit 1; }
echo "built selfhost/mfl-deadlock"

run() { # file label
  $N "$MACHIN" deadlocktest --program "$1" > "$T/o.txt" 2>/dev/null
  $N ./selfhost/mfl-deadlock --program "$1" > "$T/m.txt" 2>/dev/null
  if diff -q "$T/o.txt" "$T/m.txt" >/dev/null; then pass=$((pass+1)); echo "ok   $2"; else
    fail=$((fail+1)); echo "MISMATCH: $2"; echo " oracle:"; cat "$T/o.txt"; echo " mfl:"; cat "$T/m.txt"; fi
}

# DL001 findings (a receive on a never-fed channel).
printf 'func main(){ch:=make(chan int) v:=<-ch println(str(v))}\n' > "$T/d1"; run "$T/d1" "main recv nobody sends (DL001)"
printf 'func worker(c){}\nfunc main(){ch:=make(chan int) go worker(ch) v:=<-ch println(str(v))}\n' > "$T/d2"; run "$T/d2" "goroutine never sends (DL001)"
printf 'func main(){ch:=make(chan int) s:=0 for v:=range ch{s=s+v} println(str(s))}\n' > "$T/d3"; run "$T/d3" "range over never-fed (DL001)"
printf 'func main(){a:=make(chan int) b:=make(chan int) x:=<-a y:=<-b println(str(x+y))}\n' > "$T/d4"; run "$T/d4" "two unfed channels (2x DL001)"

# clean: fed by send / close / goroutine / indirect call chain / select send.
printf 'func send(c){c<-7}\nfunc main(){ch:=make(chan int) go send(ch) v:=<-ch println(str(v))}\n' > "$T/c1"; run "$T/c1" "goroutine sends (clean)"
printf 'func prod(c){i:=0 for i<3{c<-i i=i+1} close(c)}\nfunc main(){ch:=make(chan int) go prod(ch) s:=0 for v:=range ch{s=s+v} println(str(s))}\n' > "$T/c2"; run "$T/c2" "producer closes (clean)"
printf 'func inner(c){c<-42}\nfunc outer(c){inner(c)}\nfunc main(){ch:=make(chan int) go outer(ch) v:=<-ch println(str(v))}\n' > "$T/c3"; run "$T/c3" "indirect feed via call chain (clean)"
printf 'func a(x,y){v:=<-x y<-v}\nfunc main(){p:=make(chan int) q:=make(chan int) go a(p,q) r:=<-q p<-r println("done")}\n' > "$T/c4"; run "$T/c4" "mutual wait — both fed, runtime-only (clean)"
printf 'func feed(c){c<-1}\nfunc main(){a:=make(chan int) b:=make(chan int) go feed(a) go feed(b) select{case v:=<-a: println(str(v)) case b<-9: println("s")}}\n' > "$T/c5"; run "$T/c5" "select recv+send (clean)"
printf 'func feed(c){c<-1 close(c)}\nfunc main(){ch:=make(chan int) go feed(ch) n:=0 while n<1{arena{v,ok:=<-ch if ok{println(str(v))}} n=n+1}}\n' > "$T/c6"; run "$T/c6" "comma-ok recv under arena/while/if (clean)"

echo
echo "self-hosted deadlock oracle-diff: $pass pass, $fail fail"
rm -rf "$T"
[ "$fail" -eq 0 ]
