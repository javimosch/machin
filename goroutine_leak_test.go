package main

// #724: a finished goroutine's id path is freed. Messages it left queued must
// not depend on it, and a goroutine must still never receive its own sends.

import (
	"strings"
	"testing"
)

func TestMessagesOutliveTheirSender(t *testing.T) {
	prog, err := wallsCompose(t, `
func send3(c, base) { i := 0  while i < 3 { c <- base + i  i = i + 1 } }
func echo(c, done) { c <- 1  x := <-c  done <- x }
func main() {
    c := make(chan int)
    i := 0
    while i < 50 { go send3(c, i * 10)  i = i + 1 }
    sleep(200)
    sum := 0
    i = 0
    while i < 150 { sum = sum + <-c  i = i + 1 }
    println(sum)
    // a goroutine's own send is not deliverable to it: it waits for main's
    r := make(chan int)
    done := make(chan int)
    go echo(r, done)
    sleep(100)
    println(<-r)
    r <- 2
    println(<-done)
}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(strings.Fields(out), " "), "36900 1 2"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
