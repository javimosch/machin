package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
)

// imapProg composes framework/imap.src + the test app.
func imapProg(t *testing.T, app string) *Program {
	t.Helper()
	src, err := os.ReadFile("framework/imap.src")
	if err != nil {
		t.Skip("framework/imap.src not found")
	}
	prog, perr := progFromSrcErr(string(src) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// imapServe plays one scripted IMAP session: greeting, CAPABILITY/STARTTLS is
// NOT advertised (the test uses plaintext), LOGIN, LIST, SELECT, SEARCH, FETCH
// (with a literal), LOGOUT. Commands arrive tag-numbered a1..aN; each expected
// command gets its canned untagged+tagged response.
func imapServe(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	fmt.Fprint(w, "* OK IMAP4rev1 machin-test ready\r\n")
	w.Flush()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		// the tag is everything before the first space
		sp := strings.Index(line, " ")
		if sp < 0 {
			return
		}
		tag := line[:sp]
		cmd := strings.TrimSpace(line[sp+1:])
		switch {
		case strings.HasPrefix(cmd, "CAPABILITY"):
			fmt.Fprintf(w, "* CAPABILITY IMAP4rev1 UIDPLUS\r\n%s OK CAPABILITY completed\r\n", tag)
		case strings.HasPrefix(cmd, "STARTTLS"):
			fmt.Fprintf(w, "%s NO STARTTLS not supported\r\n", tag)
		case strings.HasPrefix(cmd, "LOGIN"):
			fmt.Fprintf(w, "%s OK LOGIN completed\r\n", tag)
		case strings.HasPrefix(cmd, "LIST"):
			fmt.Fprint(w, "* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n")
			fmt.Fprint(w, "* LIST (\\HasNoChildren) \"/\" \"Sent Items\"\r\n")
			fmt.Fprint(w, "* LIST (\\HasNoChildren) \"/\" \"Archives/2024\"\r\n")
			fmt.Fprintf(w, "%s OK LIST completed\r\n", tag)
		case strings.HasPrefix(cmd, "SELECT"):
			fmt.Fprint(w, "* FLAGS (\\Seen \\Answered \\Deleted)\r\n")
			fmt.Fprint(w, "* 3 EXISTS\r\n")
			fmt.Fprint(w, "* OK [UIDVALIDITY 777] UIDs valid\r\n")
			fmt.Fprintf(w, "%s OK [READ-WRITE] SELECT completed\r\n", tag)
		case strings.HasPrefix(cmd, "SEARCH"):
			fmt.Fprint(w, "* SEARCH 1 3\r\n")
			fmt.Fprintf(w, "%s OK SEARCH completed\r\n", tag)
		case strings.HasPrefix(cmd, "FETCH"):
			// a literal {11} + CRLF + 11 bytes + CRLF inside the FETCH response
			fmt.Fprint(w, "* 1 FETCH (FLAGS (\\Seen) BODY[HEADER] {11}\r\n")
			fmt.Fprint(w, "From: bob\r\n")
			fmt.Fprint(w, ")\r\n")
			fmt.Fprintf(w, "%s OK FETCH completed\r\n", tag)
		case strings.HasPrefix(cmd, "LOGOUT"):
			fmt.Fprint(w, "* BYE logging out\r\n")
			fmt.Fprintf(w, "%s OK LOGOUT completed\r\n", tag)
			w.Flush()
			return
		default:
			fmt.Fprintf(w, "%s BAD unsupported\r\n", tag)
		}
		w.Flush()
	}
}

// TestImapClient exercises the pure-MFL IMAP client against a scripted loopback
// server: greeting + LOGIN, LIST -> JSON mailbox names, SELECT -> EXISTS count,
// SEARCH -> JSON seq numbers, FETCH with an inlined literal, LOGOUT.
func TestImapClient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback available: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go imapServe(c)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	app := fmt.Sprintf(`
func main() {
    c, ok := imap_connect("127.0.0.1", %d, "alice", "s3cret", 0)
    println("CONN ok=" + str(ok))
    if ok == 1 {
        println("LIST " + imap_list(c))
        sok, ex := imap_select(c, "INBOX")
        println("SELECT ok=" + str(sok) + " ex=" + str(ex))
        println("SEARCH " + imap_search(c, "FROM \"alice\""))
        raw, fok := imap_fetch(c, "1", "FLAGS ENVELOPE BODY[HEADER]")
        println("FETCH ok=" + str(fok))
        println("RAW<" + raw + ">")
        println("LO=" + str(imap_logout(c)))
    }
    // bad login must fail closed
    c2, ok2 := imap_connect("127.0.0.1", %d, "alice", "wrong", 0)
    println("CONN2 (server accepts any login here) ok=" + str(ok2))
    if ok2 == 1 { imap_logout(c2) }
    // unreachable host fails
    c3, ok3 := imap_connect("127.0.0.1", 1, "u", "p", 0)
    println("CONN3 ok=" + str(ok3))
}`, port, port)

	out, err := RunCaptured(imapProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"CONN ok=1",
		`LIST ["INBOX","Sent Items","Archives/2024"]`,
		"SELECT ok=1 ex=3",
		`SEARCH [1,3]`,
		"FETCH ok=1",
		"From: bob",
		"LO=0",
		"CONN3 ok=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing expectation %q; got:\n%s", want, out)
		}
	}
}
