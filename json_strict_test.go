package main

// #715 (json() escaping) and #716 (json_valid).

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// mflBytes renders a Go string as an MFL expression yielding the same bytes, so
// control characters and invalid UTF-8 reach the program untouched. NUL cannot
// live in an MFL string and is not used.
func mflBytes(s string) string {
	var b strings.Builder
	b.WriteString(`url_decode("`)
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "%%%02X", s[i])
	}
	b.WriteString(`")`)
	return b.String()
}

func TestJSONEscapesControlCharacters(t *testing.T) {
	var all []byte
	for c := 1; c < 0x20; c++ {
		all = append(all, byte(c))
	}
	// Dense runs of 6-byte escapes also exercise the output buffer sizing.
	inputs := []string{string(all), strings.Repeat("\x01", 5000), "a\"b\\c\bd\fe\x1f é"}
	src := "type T struct { s string }\nfunc main() {\n"
	for i, in := range inputs {
		src += fmt.Sprintf("j%d := json(%s)\nprintln(j%d)\n", i, mflBytes(in), i)
		// round trip: parse must decode what json() writes
		src += fmt.Sprintf("t%d := parse(\"{\\\"s\\\":\" + j%d + \"}\", T{})\nprintln(json_valid(j%d), t%d.s == %s)\n", i, i, i, i, mflBytes(in))
	}
	prog, err := wallsCompose(t, src+"}\n")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "esc")
	if err := BuildBinary(prog, bin, true); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2*len(inputs) {
		t.Fatalf("got %d lines: %q", len(lines), lines)
	}
	for i, in := range inputs {
		var got string
		if err := json.Unmarshal([]byte(lines[2*i]), &got); err != nil || got != in {
			t.Errorf("input %d: err=%v output=%q", i, err, lines[2*i])
		}
		if lines[2*i+1] != "true true" {
			t.Errorf("input %d: valid/roundtrip = %s", i, lines[2*i+1])
		}
	}
}

func TestJSONValid(t *testing.T) {
	cases := []string{
		`{}`, `[]`, `null`, `true`, `false`, `0`, `-0`, `12`, `-1.5e+10`, `2E-3`, `""`, `"aé\n\/"`,
		" \t\r\n{ \"a\" : [ 1 , { \"b\" : null } ] } \n", `"é € 𝄞"`, `"\ud800"`,
		``, ` `, `not json`, `{"name":"a"`, `{"n":1,`, `{"n":1}garbage`, `{} {}`, `[1,]`, `{"a":1,}`,
		`{a:1}`, `{"a" 1}`, `{"a":}`, `[1 2]`, `01`, `1.`, `.5`, `+1`, `1e`, `1e+`, `-`, `NaN`,
		`Infinity`, `tru`, `nulls`, `truex`, `'a'`, `"abc`, `"\x"`, `"\u12"`, `"\u12g4"`, `"\`,
		"\"a\tb\"", "\"a\x01\"", "[1]\x0b", "\xef\xbb\xbf{}", `[}`, `{]`, `]`, `,`,
	}
	badUTF8 := []string{"\"\xff\"", "\"\xc0\xaf\"", "\"\xe2\x82\"", "\"\xed\xa0\x80\"", "\"\xf4\x90\x80\x80\""}
	cases = append(cases, badUTF8...)
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	deep := []string{nest(512), nest(513), strings.Repeat("[", 200000)}
	cases = append(cases, deep...)
	want := make([]bool, len(cases))
	src := "func main() {\n"
	for i, c := range cases {
		// encoding/json is the oracle, except for UTF-8 (it is lenient) and our depth cap.
		want[i] = json.Valid([]byte(c)) && utf8.ValidString(c)
		if c == nest(513) {
			want[i] = false
		}
		src += "println(json_valid(" + mflBytes(c) + "))\n"
	}
	lines := strings.Fields(runMFL(t, src+"}\n"))
	if len(lines) != len(cases) {
		t.Fatalf("got %d results for %d cases", len(lines), len(cases))
	}
	for i, c := range cases {
		if got := lines[i] == "true"; got != want[i] {
			t.Errorf("json_valid(%.60q) = %v, want %v", c, got, want[i])
		}
	}
}
