package main

// Regression tests for #747 (one writer at a time per connection) and #748
// (a 16-bit frame length of 127 in framework/wsclient.src).

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// Two goroutines write large messages while the reading goroutine answers pings,
// to a peer that is behind on reading. Every frame must arrive whole.
const wsBusyApp = `
func blast(fd, ch, done) {
    chunk := ch
    for len(chunk) < 200000 { chunk = chunk + chunk }
    i := 0
    while i < 40 {
        n := ws_send_text(fd, chunk)
        i = i + 1
    }
    done <- 1
}

func handle(req) (res) {
    res = ws(req, func(c) {
        done := make(chan int)
        if req.path == "/stuck" || req.path == "/busy" {
            go blast(c.fd, "a", done)
            go blast(c.fd, "b", done)
        }
        ok := 1
        while ok == 1 {
            arena {
                msg, got := ws_next_text(c)
                ok = got
                if got == 1 && msg == "finish" {
                    x := <-done
                    y := <-done
                    n := ws_send_text(c.fd, "done")
                }
                if got == 1 && msg == "hello" { n := ws_send_text(c.fd, "hello") }
            }
        }
    })
}
`

func TestWebSocketWritesToOneConnectionDoNotInterleave(t *testing.T) {
	addr, _ := startWSServer(t, wsBusyApp)

	dial := func(path string) *wsPeer {
		return dialWSPath(t, addr, path)
	}
	// a peer that never reads must not hold up writes to anyone else
	dial("/stuck")
	time.Sleep(200 * time.Millisecond)
	other := dialWSPath(t, addr, "/")
	other.c.SetDeadline(time.Now().Add(10 * time.Second))
	other.send(0x81, "hello")
	other.expect("1:hello")

	for trial := 0; trial < 3; trial++ {
		p := dial("/busy")
		stop := make(chan struct{})
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			for i := 0; i < 300; i++ {
				if _, err := p.c.Write(wsClientFrame(0x89, []byte("marker"), true)); err != nil {
					return
				}
				select {
				case <-stop:
					return
				case <-time.After(time.Millisecond):
				}
			}
		}()
		time.Sleep(100 * time.Millisecond) // fall behind
		a, b := 0, 0
		for a+b < 80 {
			got := p.read()
			switch {
			case got == "10:marker":
			case strings.HasPrefix(got, "1:a") && strings.Count(got, "a") == len(got)-2:
				a++
				time.Sleep(time.Millisecond)
			case strings.HasPrefix(got, "1:b") && strings.Count(got, "b") == len(got)-2:
				b++
				time.Sleep(time.Millisecond)
			default:
				t.Fatalf("trial %d: corrupt stream after %d messages: %.80q", trial, a+b, got)
			}
		}
		close(stop)
		<-sent
		p.send(0x81, "finish")
		for got := p.read(); got != "1:done"; got = p.read() {
			if got != "10:marker" {
				t.Fatalf("trial %d: %.80q", trial, got)
			}
		}
		p.c.Close()
	}
}

// The ws_* client builtins have the same two writers: ws_send from one goroutine
// and the pong that ws_recv sends from another.
func TestWebSocketClientBuiltinWritesDoNotInterleave(t *testing.T) {
	result := make(chan error, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		result <- func() error {
			c, err := ln.Accept()
			if err != nil {
				return err
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(30 * time.Second))
			if _, err := wsReadHTTPHeaders(c); err != nil {
				return err
			}
			io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\n\r\n")
			go func() {
				for i := 0; i < 300; i++ {
					if _, err := c.Write([]byte("\x89\x06marker")); err != nil {
						return
					}
					time.Sleep(time.Millisecond)
				}
			}()
			time.Sleep(100 * time.Millisecond)
			for n := 0; n < 40; {
				_, op, pl, err := wsReadFrame(c)
				if err != nil {
					return fmt.Errorf("after %d messages: %v", n, err)
				}
				switch {
				case op == 10 && string(pl) == "marker":
				case op == 1 && len(pl) >= 200000 && strings.Count(string(pl), "a") == len(pl):
					n++
					time.Sleep(time.Millisecond)
				default:
					return fmt.Errorf("corrupt stream after %d messages: opcode %d, %.60q", n, op, pl)
				}
			}
			_, err = c.Write([]byte("\x81\x04done"))
			return err
		}()
	}()
	src := fmt.Sprintf(`
func blast(h, done) {
    chunk := "a"
    for len(chunk) < 200000 { chunk = chunk + chunk }
    i := 0
    while i < 40 {
        n := ws_send(h, chunk)
        i = i + 1
    }
    done <- 1
}
func main() {
    h := ws_open("ws://%s/")
    done := make(chan int)
    go blast(h, done)
    println(ws_recv(h))
    x := <-done
}
`, ln.Addr().String())
	out, err := RunCaptured(progFromSrc(t, src))
	if serr := <-result; serr != nil {
		t.Fatal(serr)
	}
	if err != nil || strings.TrimSpace(out) != "done" {
		t.Fatalf("client: %v %q", err, out)
	}
}

func TestWsClientFrameLengthBoundaries(t *testing.T) {
	sizes := []int{125, 126, 127, 128, 65535, 65536}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(30 * time.Second))
		head, err := wsReadHTTPHeaders(c)
		if err != nil {
			return
		}
		key := ""
		for _, line := range strings.Split(head, "\r\n") {
			if v, ok := strings.CutPrefix(line, "Sec-WebSocket-Key: "); ok {
				key = v
			}
		}
		io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nSec-WebSocket-Accept: "+wsAcceptKey(key)+"\r\n\r\n")
		for _, n := range sizes {
			f := wsClientFrame(0x81, []byte(strings.Repeat("x", n)), false)
			c.Write(f)
		}
		io.Copy(io.Discard, c)
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	src := fmt.Sprintf(`
func main() {
    c, ok := wsc_open("127.0.0.1", %s, "/")
    i := 0
    while i < %d {
        msg, good := wsc_recv(c)
        println(str(good) + " " + str(len(msg)))
        i = i + 1
    }
    n := wsc_close(c)
}
`, port, len(sizes))
	out, err := RunCaptured(wsclientProg(t, src))
	want := ""
	for _, n := range sizes {
		want += fmt.Sprintf("1 %d\n", n)
	}
	if err != nil || out != want {
		t.Fatalf("%v: got %q, want %q", err, out, want)
	}
}
