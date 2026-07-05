package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestBase64DecodeLenientEdgeCases covers mfl_base64_decode (codegen.go), a
// lenient decoder that accepts both standard and URL-safe alphabets and
// silently skips padding/whitespace/invalid bytes rather than erroring. These
// properties are what let it double as a JWT-segment decoder, but weren't
// exercised beyond the single happy-path round trip in mfl_test.go.
func TestBase64DecodeLenientEdgeCases(t *testing.T) {
	raw := "a?/b+c" // contains bytes that b64-encode to both '+' and '/'
	std := base64.StdEncoding.EncodeToString([]byte(raw))
	urlSafe := strings.NewReplacer("+", "-", "/", "_").Replace(std)
	unpadded := strings.TrimRight(std, "=")
	spaced := strings.Join(strings.Split(std, ""), " ")

	got := runNative(t, `func main(){
        println(base64_decode(""))
        println(base64_decode("`+std+`"))
        println(base64_decode("`+urlSafe+`"))
        println(base64_decode("`+unpadded+`"))
        println(base64_decode("`+spaced+`"))
    }`)
	want := "\n" + raw + "\n" + raw + "\n" + raw + "\n" + raw + "\n"
	if got != want {
		t.Fatalf("base64_decode edge cases: got %q, want %q", got, want)
	}
}
