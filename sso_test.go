package main

import (
	"os"
	"strings"
	"testing"
)

// End-to-end OAuth2/OIDC SSO (framework/sso.src) against a mock identity provider
// served IN-PROCESS by machweb: a real http_request token exchange + userinfo GET
// over localhost, then the full callback flow (state/CSRF check + exchange + profile)
// with a signed-cookie state. No external dependency — CI-runnable.
func TestSSOFlow(t *testing.T) {
	mw, err := os.ReadFile("framework/machweb.src")
	if err != nil {
		t.Skip("framework/machweb.src not found")
	}
	sso, err := os.ReadFile("framework/sso.src")
	if err != nil {
		t.Skip("framework/sso.src not found")
	}
	app := `
func mock(req) (res) {
    if has_prefix(req.path, "/token") {
        res = ok_json("{\"access_token\":\"tok-123\",\"token_type\":\"Bearer\"}")
    } else {
        if has_prefix(req.path, "/userinfo") {
            if header(req, "authorization") == "Bearer tok-123" {
                res = ok_json("{\"sub\":\"u42\",\"email\":\"ada@x.com\",\"name\":\"Ada\"}")
            } else {
                res = bad_request("no-auth")
            }
        } else {
            res = not_found()
        }
    }
}
func main() {
    go serve(48306, func(req) { return mock(req) })
    sleep(120)
    p := OAuthProvider{auth_url: "http://localhost:48306/auth", token_url: "http://localhost:48306/token", userinfo_url: "http://localhost:48306/userinfo", client_id: "cid", client_secret: "sec", redirect_uri: "http://app/cb", scope: "openid email"}
    println("login=" + sso_login_url(p, "st8"))
    secret := "shh"
    state := "deadbeef"
    signed := session_sign(secret, state)
    // happy path: state matches the signed cookie -> exchange + profile
    okreq := parse_request("GET /cb?code=c1&state=" + state + " HTTP/1.1\r\nCookie: oauth_state=" + signed + "\r\n\r\n")
    prof, ok := sso_complete(p, secret, okreq)
    email, _ := json_get(prof, ".email")
    println("ok=" + str(ok) + " email=" + email)
    // CSRF: a mismatched state is rejected before any exchange
    badreq := parse_request("GET /cb?code=c1&state=wrong HTTP/1.1\r\nCookie: oauth_state=" + signed + "\r\n\r\n")
    _, bad := sso_complete(p, secret, badreq)
    println("csrf_ok=" + str(bad))
    // forged state cookie (no valid HMAC) is rejected
    _, forged := sso_complete(p, secret, parse_request("GET /cb?code=c1&state=x HTTP/1.1\r\nCookie: oauth_state=x.deadbeef\r\n\r\n"))
    println("forged_ok=" + str(forged))
}`
	prog, perr := progFromSrcErr(string(mw) + string(sso) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"login=http://localhost:48306/auth?response_type=code&client_id=cid", // url built
		"redirect_uri=http%3A%2F%2Fapp%2Fcb",                                 // url-encoded
		`ok=1 email="ada@x.com"`,                                             // exchange + userinfo succeeded
		"csrf_ok=0",                                                          // state mismatch blocked
		"forged_ok=0",                                                        // unsigned state cookie blocked
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
