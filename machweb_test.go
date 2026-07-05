package main

import (
	"os"
	"strings"
	"testing"
)

// machwebProg composes framework/machweb.src (the server framework) with a test
// app and parses it. machweb has no extern block — its net builtins (listen/accept/
// read/write) compile but are only reached from serve(), which the tests never call,
// so the pure request/response/router logic runs natively under RunCaptured.
func machwebProg(t *testing.T, app string) *Program {
	t.Helper()
	data, err := os.ReadFile("framework/machweb.src")
	if err != nil {
		t.Skip("framework/machweb.src not found")
	}
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// parse_request must split the request line, extract the body after the blank line,
// and build a lowercased header map; header() is case-insensitive and "" when absent;
// param() returns the path segment after a prefix.
func TestMachwebParseRequest(t *testing.T) {
	app := `
func main() {
    raw := "POST /users/42 HTTP/1.1\r\nHost: ex\r\nContent-Type: application/json\r\nContent-Length: 7\r\n\r\n{\"a\":1}"
    req := parse_request(raw)
    println("method=" + req.method)
    println("path=" + req.path)
    println("body=" + req.body)
    println("ctype=" + header(req, "Content-Type"))
    println("clen=" + header(req, "content-length"))
    println("host=" + header(req, "HOST"))
    println("missing=[" + header(req, "X-Nope") + "]")
    println("param=" + param(req.path, "/users/"))
    println("noprefix=[" + param(req.path, "/posts/") + "]")
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"method=POST", "path=/users/42", "body={\"a\":1}",
		"ctype=application/json", // header name is case-insensitive
		"clen=7",
		"host=ex",
		"missing=[]",  // absent header -> ""
		"param=42",    // segment after the prefix
		"noprefix=[]", // prefix that doesn't match -> ""
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// content_length reads the Content-Length value from a header block (and is 0 when
// the header is absent) — this is what read_request uses to wait for a full body.
func TestMachwebContentLength(t *testing.T) {
	app := `
func main() {
    println("a=" + str(content_length("GET / HTTP/1.1\r\nContent-Length: 42\r\nHost: x\r\n")))
    println("b=" + str(content_length("GET / HTTP/1.1\r\nHost: x\r\n")))
    println("c=" + str(content_length("POST / HTTP/1.1\r\ncontent-length: 5\r\n")))
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"a=42", "b=0", "c=5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The response builders set the right status line and content type, and is_bin=0
// for text bodies (so machweb_handle writes the text path, not the bytes path).
func TestMachwebResponseBuilders(t *testing.T) {
	app := `
func main() {
    j := ok_json("[]")
    println("json=" + j.status + "|" + j.ctype + "|" + str(j.is_bin))
    h := ok_html("<h1>")
    println("html=" + h.ctype)
    println("nf=" + not_found().status)
    b := bad_request("nope")
    println("bad=" + b.status + "|" + b.body)
    println("created=" + created("{}").status)
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"json=200 OK|application/json|0",
		"html=text/html; charset=utf-8",
		"nf=404 Not Found",
		"bad=400 Bad Request|nope",
		"created=201 Created",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Cookies: parse named values from the request's Cookie header. Signed sessions:
// value + HMAC tag; verify accepts the intact cookie and rejects a tampered tag or
// the wrong secret — the client can read the value but cannot forge it.
func TestMachwebCookiesAndSessions(t *testing.T) {
	app := `
func main() {
    req := parse_request("GET / HTTP/1.1\r\nCookie: sid=abc; theme=dark\r\n\r\n")
    println("sid=" + cookie(req, "sid"))
    println("theme=" + cookie(req, "theme"))
    println("missing=[" + cookie(req, "nope") + "]")
    signed := session_sign("k", "user:42")
    v, ok := session_verify("k", signed)
    println("verify=" + v + ":" + str(ok))
    bad := substr(signed, 0, len(signed) - 1) + "0"
    _, t2 := session_verify("k", bad)
    println("tampered=" + str(t2))
    _, w := session_verify("wrong", signed)
    println("wrongsecret=" + str(w))
    // a Set-Cookie line carries the signed value + safe attributes
    r := set_session(ok_html("x"), "k", "sid", "user:42")
    println("setcookie=" + cookie_lines(r))
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"sid=abc", "theme=dark", "missing=[]",
		"verify=user:42:1",                   // intact cookie verifies, value recovered
		"tampered=0",                         // a flipped tag byte fails
		"wrongsecret=0",                      // a different secret fails
		"setcookie=Set-Cookie: sid=user:42.", // signed value embeds the cleartext
		"HttpOnly; SameSite=Lax",             // safe defaults
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The map-based router dispatches "METHOD PATH" to its handler closure and falls
// back to 404 for an unregistered route.
func TestMachwebRouterDispatch(t *testing.T) {
	app := `
func mkreq(method, path) (r) { r = Request{method: method, path: path, body: "", headers: make(map[string]string)} }
func main() {
    r := new_router()
    route(r, "GET", "/", func(req) { return ok_text("home") })
    route(r, "GET", "/users", func(req) { return ok_json("[1,2]") })
    route(r, "POST", "/users", func(req) { return created("{}") })
    println("get_root=" + dispatch(r, mkreq("GET", "/")).body)
    println("get_users=" + dispatch(r, mkreq("GET", "/users")).body)
    println("post_users=" + dispatch(r, mkreq("POST", "/users")).status)
    println("unknown=" + dispatch(r, mkreq("DELETE", "/x")).status)
    println("wrong_method=" + dispatch(r, mkreq("PUT", "/users")).status)
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"get_root=home",
		"get_users=[1,2]",
		"post_users=201 Created", // method is part of the key
		"unknown=404 Not Found",
		"wrong_method=404 Not Found", // GET /users registered, PUT /users is not
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
