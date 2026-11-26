package main

// Every hand-kept copy of json() string escaping must agree with the runtime.
// #715 fixed mfl_json_str; certify's Go mirror and the frameworks' own
// escapers were separate copies.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func allControlBytes() string {
	var b []byte
	for c := 1; c < 0x20; c++ {
		b = append(b, byte(c))
	}
	return string(b) + "\"\\ é\x7f" + "\x01\x01"
}

func composeAndRun(t *testing.T, src string, frameworks ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.src")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, _, err := composeSources(append(frameworks, p))
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(out, "\n")
}

// certify compares its own rendering with the compiled json() output, so any
// difference is reported as a miscompile.
func TestCertifyJSONEscapeMatchesRuntime(t *testing.T) {
	in := allControlBytes()
	got := composeAndRun(t, "func main() { println(json("+mflBytes(in)+")) }")
	if want := jsonEscape(in); got != want {
		t.Fatalf("runtime %q\ncertify %q", got, want)
	}
}

func TestFrameworkJSONEscapers(t *testing.T) {
	in := allControlBytes()
	for _, c := range []struct{ fn, file string }{
		{"bs_jstr", "bson"}, {"caldav_jstr", "caldav"}, {"jstr", "mysql"},
		{"redis_jstr", "redis"}, {"json_str", "postgres"}, {"json_quote", "json"},
	} {
		t.Run(c.file, func(t *testing.T) {
			deps := []string{"framework/" + c.file + ".src"}
			if c.file == "caldav" {
				deps = append([]string{"framework/xml.src"}, deps...)
			}
			out := composeAndRun(t, fmt.Sprintf("func main() { println(%s(%s)) }", c.fn, mflBytes(in)), deps...)
			var got string
			if err := json.Unmarshal([]byte(out), &got); err != nil || got != in {
				t.Errorf("%s: err=%v output=%q", c.fn, err, out)
			}
		})
	}
}
