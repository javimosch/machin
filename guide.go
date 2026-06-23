package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// machinVersion is the single version string for the toolchain. Bump it when
// cutting a release (alongside README badge / SPEC / CHANGELOG).
const machinVersion = "0.29.0"

// ---- the source-of-truth feature catalog ----
//
// This catalog IS the machine-readable reference an agent loads to master machin
// in one call: `machin guide` emits it as JSON (default) or `--text` (dense
// prose). It lives in the binary so it is always version-exact and cannot drift
// from the implementation; a test (guide_test.go) asserts every builtin here
// actually type-checks, so the catalog stays honest.

type guideBuiltin struct {
	Name     string `json:"name"`
	Sig      string `json:"sig"`
	Summary  string `json:"summary"`
	Category string `json:"category"`
}

type guideIdiom struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type guideNote struct {
	Topic string `json:"topic"`
	Note  string `json:"note"`
}

type guideCatalog struct {
	Version  string         `json:"version"`
	Schema   string         `json:"schema"`
	Tagline  string         `json:"tagline"`
	Keywords []string       `json:"keywords"`
	Types    []guideNote    `json:"types"`
	Builtins []guideBuiltin `json:"builtins"`
	Idioms   []guideIdiom   `json:"idioms"`
	Gotchas  []guideNote    `json:"gotchas"`
}

func machinGuide() guideCatalog {
	return guideCatalog{
		Version: machinVersion,
		Schema:  "machin.guide/v1",
		Tagline: "Go-flavored, type-inferred, machine-first language; MFL compiles through C to a single native binary. Plain-text source, one declaration per line.",
		Keywords: []string{
			"func", "return", "if", "else", "while", "for", "range", "break", "continue",
			"select", "go", "chan", "make", "map", "struct", "type", "var", "arena",
			"extern", "true", "false", "nil",
		},
		Types: []guideNote{
			{"int", "64-bit signed"},
			{"float", "double"},
			{"bool", "true/false"},
			{"string", "immutable UTF-8 bytes; zero value \"\""},
			{"[]T", "slice (append to grow); for i, v := range"},
			{"map[K]V", "K is int or string; make(map[K]V); has/delete/keys"},
			{"struct", "type T struct { f T ... }; value semantics; T{f: v}"},
			{"chan T", "make(chan T); ch <- v; <-ch; close; for v := range ch; v, ok := <-ch"},
			{"func", "first-class closures; captured by reference"},
		},
		Builtins: []guideBuiltin{
			// io
			{"print", "(...) ->", "write args, no trailing newline", "io"},
			{"println", "(...) ->", "write args + trailing newline", "io"},
			{"input", "() -> string", "read one stdin line (newline stripped; \"\" at EOF)", "io"},
			{"read_stdin", "() -> string", "read all of stdin verbatim until EOF (exact bytes; no line splitting)", "io"},
			{"flush", "() ->", "flush buffered stdout (prompt output through a pipe)", "io"},
			{"read_file", "(string) -> string", "read a whole file (\"\" on error)", "io"},
			{"write_file", "(string, string) -> int", "write a file (bytes; -1 on error)", "io"},
			{"list_dir", "(string) -> []string", "directory entries (excludes . / ..)", "io"},
			{"mkdir", "(string) -> int", "create a directory (0 ok; -1 error)", "io"},
			// cli / process
			{"args", "() -> []string", "command-line args (args()[0] is program path)", "cli"},
			{"env", "(string) -> string", "environment variable (\"\" if unset)", "cli"},
			{"exit", "(int) ->", "terminate the process with a status code", "cli"},
			// time
			{"now", "() -> int", "wall-clock Unix seconds", "time"},
			{"now_ms", "() -> int", "wall-clock milliseconds", "time"},
			{"sleep", "(int) ->", "pause for N milliseconds", "time"},
			{"time_fields", "(int) -> []int", "decompose a unix timestamp (local) -> [year,month,day,hour,min,sec,weekday(0=Sun),yearday]", "time"},
			{"time_format", "(int, string) -> string", "format a unix timestamp (local) with a strftime pattern (%Y %m %d %H %M %S %A %B %z %Z %F %T ...)", "time"},
			// convert
			{"str", "(int|float) -> string", "format a number", "convert"},
			{"int", "(number) -> int", "truncate to int", "convert"},
			{"parse_int", "(string) -> int", "parse an integer (0 if non-numeric)", "convert"},
			// collections
			{"len", "(string|slice|map) -> int", "length", "collection"},
			{"append", "([]T, T) -> []T", "grow a slice", "collection"},
			{"has", "(map, K) -> bool", "key membership", "collection"},
			{"delete", "(map, K) ->", "remove a key", "collection"},
			{"keys", "(map[K]V) -> []K", "a map's keys", "collection"},
			// strings
			{"substr", "(string, int, int) -> string", "substring [start,end)", "string"},
			{"index", "(string, string) -> int", "first index, or -1", "string"},
			{"contains", "(string, string) -> bool", "substring test", "string"},
			{"has_prefix", "(string, string) -> bool", "prefix test", "string"},
			{"has_suffix", "(string, string) -> bool", "suffix test", "string"},
			{"charat", "(string, int) -> string", "1-character string", "string"},
			{"to_upper", "(string) -> string", "uppercase", "string"},
			{"to_lower", "(string) -> string", "lowercase", "string"},
			{"trim", "(string) -> string", "trim surrounding whitespace", "string"},
			{"replace", "(string, string, string) -> string", "replace all", "string"},
			{"split", "(string, string) -> []string", "split on a separator", "string"},
			{"join", "([]string, string) -> string", "join with a separator", "string"},
			{"base64_encode", "(string) -> string", "base64-encode text (standard, padded)", "string"},
			{"base64_decode", "(string) -> string", "base64-decode (lenient: standard + url-safe; ignores padding)", "string"},
			{"sha256", "(string) -> string", "SHA-256 of text, lowercase hex", "crypto"},
			{"hmac_sha256", "(string, string) -> string", "HMAC-SHA256(key, message), lowercase hex (webhook signatures)", "crypto"},
			// sqlite (libsqlite3, linked only when used)
			{"sqlite_open", "(string) -> int", "open/create a SQLite db file -> handle (0 on fail); \":memory:\" for in-memory", "db"},
			{"sqlite_exec", "(int, string[, []string]) -> int", "run SQL with no result; optional []string binds the ? params (injection-safe); 0 ok", "db"},
			{"sqlite_query", "(int, string[, []string]) -> string", "run a SELECT -> JSON array of rows; optional []string binds the ? params; composes with json_get", "db"},
			{"sqlite_close", "(int) -> int", "close the database", "db"},
			// regex (POSIX extended)
			{"regex_match", "(string, string) -> bool", "does the ERE pattern match anywhere in s", "regex"},
			{"regex_find", "(string, string) -> string", "first ERE match in s (\"\" if none)", "regex"},
			{"regex_groups", "(string, string) -> []string", "first match's groups: [0]=whole, [1..]=captures ([] if none)", "regex"},
			{"regex_replace", "(string, string, string) -> string", "replace all ERE matches in s with repl", "regex"},
			// json
			{"json", "(any) -> string", "serialize a value to JSON", "json"},
			{"parse", "(string, T{}) -> T", "parse JSON into T (T{} is a type witness)", "json"},
			{"json_get", "(string, string) -> (string, string)", "jq-style path -> (value, err); err \"\"/notfound/path/parse. MULTI-ASSIGN ONLY", "json"},
			{"http_body", "(string) -> string", "body of a raw HTTP message", "json"},
			// net
			{"dial", "(string, int) -> int", "TCP connect host:port -> fd (-1 on fail)", "net"},
			{"listen", "(int) -> int", "open a listening TCP socket on a port", "net"},
			{"accept", "(int) -> int", "accept a connection -> fd", "net"},
			{"read", "(int) -> string", "read a chunk from an fd (blocks)", "net"},
			{"write", "(int, string) -> int", "write to an fd", "net"},
			{"close", "(int|chan) ->", "close an fd, or a channel (by argument type)", "net"},
			{"https_get", "(string) -> string", "HTTPS GET over TLS; body (\"\" on error)", "net"},
			{"https_post", "(string, string) -> string", "HTTPS POST (JSON body) over TLS; body", "net"},
			{"http_get", "(string) -> (int, string, string)", "GET -> (status, body, err); err \"\"/dns/connect/tls/scheme. MULTI-ASSIGN ONLY", "net"},
			// websocket
			{"wss_open", "(string) -> int", "open a wss:// WebSocket -> handle (0 on fail)", "ws"},
			{"wss_send", "(int, string) -> int", "send a text message", "ws"},
			{"wss_recv", "(int) -> string", "next message (blocks; \"\" on close; auto ping/pong)", "ws"},
			{"wss_close", "(int) -> int", "send close and tear down", "ws"},
		},
		Idioms: []guideIdiom{
			{"hello", `func main() { println("hello") }`},
			{"types", `type P struct { name string  age int }
func main() { p := P{name: "ada", age: 36}  xs := []int{1, 2, 3}  m := make(map[string]int)  m["k"] = 1  println(p.name + " " + str(len(xs)) + " " + str(m["k"])) }`},
			{"goroutine-channel", `func work(ch) { ch <- 42 }
func main() { ch := make(chan int)  go work(ch)  println(str(<-ch)) }`},
			{"select-timeout", `func after(ms, ch) { sleep(ms)  ch <- true }
func main() { done := make(chan int)  t := make(chan bool)  go after(100, t)
	select { case v := <-done: println(str(v))  case <-t: println("timeout") } }`},
			{"worker-pool-close", `func worker(jobs, out) { for u := range jobs { out <- u + "!" }  out <- "done" }
func main() { jobs := make(chan string)  out := make(chan string)  go worker(jobs, out)
	jobs <- "a"  jobs <- "b"  close(jobs)
	for { r := <- out  if r == "done" { break }  println(r) } }`},
			{"comma-ok", `func prod(ch) { ch <- 1  ch <- 2  close(ch) }
func main() { ch := make(chan int)  go prod(ch)
	for { v, ok := <- ch  if ok == false { break }  println(str(v)) } }`},
			{"error-handling", `func main() { status, body, err := http_get("https://example.com/")
	if len(err) > 0 { println("unreachable: " + err)  exit(1) }
	println(str(status) + " " + str(len(body))) }`},
			{"json-path", `func main() { body := https_get("https://api.github.com/repos/javimosch/machin")
	full, err := json_get(body, ".full_name")  if len(err) == 0 { println(full) } }`},
			{"variadic", `func sum(nums...) (t) { t = 0  for _, n := range nums { t = t + n } }
func main() { println(str(sum(1, 2, 3))) }`},
			{"named-returns", `func divmod(a, b) (q, r) { q = a / b  r = a % b  return }
func main() { q, r := divmod(17, 5)  println(str(q) + " " + str(r)) }`},
			{"closure", `func adder() (f) { n := 0  f = func(x) { n = n + x  return n } }
func main() { a := adder()  println(str(a(2)))  println(str(a(5))) }`},
			{"generic", `func id(x) (v) { v = x }
func main() { println(str(id(42)) + " " + id("hi")) }`},
			{"scoped-arena", `func main() { total := 0  n := 0
	while n < 3 { arena { s := "row-" + str(n)  total = total + len(s) }  n = n + 1 }
	println(str(total)) }`},
			{"ffi-extern", `extern "m" { cflags "-lm" header "math.h" fn sqrt(float) float }
func main() { println(str(sqrt(2.0))) }`},
		},
		Gotchas: []guideNote{
			{"build", "Author loose Go-like .src, then `machin encode a.src > a.mfl` and `machin build a.mfl -o app`. The .mfl is canonical plain text (one decl/line)."},
			{"struct-value-semantics", "Structs are VALUE types: passing or assigning one copies it, so a function cannot mutate a caller's struct (and a builder must return the updated struct). For shared mutable state use a map — a reference type, so m[k]=v survives the holder being passed by value (see framework/flags.src)."},
			{"map-comma-ok", "There is no map comma-ok: `v, ok := m[k]` does NOT compile. A read of an absent key returns the value type's zero value; use has(m, k) to test presence. (comma-ok is for channel receives: `v, ok := <-ch`.)"},
			{"parse-witness", "parse(s, T{}) needs a value of T as a type witness, e.g. `u := parse(body, User{})`. For schemaless extraction use json_get(s, path); json(x) serializes any value."},
			{"multi-assign-only", "http_get and json_get return multiple values; use `a, b, c := http_get(u)`. Calling them as a single value is a compile error."},
			{"eval-order", "Operands and arguments evaluate left-to-right (as in Go), including side effects: `f() + g()` runs f() before g(). Holds for binary ops, call args, slice/struct literals, and multi-return lists."},
			{"composite-literal", "T{...} literals need T to be a known struct type at parse time. `machin encode` registers all `type` decls first, so this just works in normal builds."},
			{"stdout-buffering", "libc fully buffers stdout when it's a pipe; a streaming program must call flush() after a write to appear promptly downstream. A TTY is line-buffered."},
			{"channels-cross-goroutine", "Values sent over a channel are deep-copied across the goroutine/arena boundary (strings fast; slices/maps/structs via JSON), so they survive the sender goroutine. Channels of closures/funcs are not deep-copied."},
			{"select-closed", "A closed channel makes its select receive case ready, firing repeatedly (with ok==false if you wrote `case v, ok := <-ch:`). Detect close and stop selecting on it."},
			{"no-tls-without-https", "There is no raw TLS socket; use https_get/https_post (REST) and wss_* (WebSocket). Plain dial/listen are TCP without TLS."},
			{"floats-over-chan-json", "A slice/map channel element round-trips through JSON, which formats floats with %g (not bit-exact for pathological doubles)."},
			{"memory", "Per-goroutine arena, reclaimed in bulk when the goroutine returns; wrap a hot allocating loop in `arena { ... }` to keep peak memory flat. Build with --safe for bounds/overflow/div-zero checks."},
		},
	}
}

// cmdGuide prints the feature catalog: JSON by default (machine-readable, the
// intended agent entry point), or a dense prose form with --text.
func cmdGuide(args []string) error {
	text := false
	for _, a := range args {
		switch a {
		case "--text", "-t":
			text = true
		case "--json":
			text = false
		}
	}
	g := machinGuide()
	if text {
		fmt.Print(renderGuideText(g))
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(g)
}

func renderGuideText(g guideCatalog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "machin %s — %s\n\n", g.Version, g.Tagline)
	fmt.Fprintf(&b, "KEYWORDS: %s\n\n", strings.Join(g.Keywords, " "))

	b.WriteString("TYPES\n")
	for _, t := range g.Types {
		fmt.Fprintf(&b, "  %-10s %s\n", t.Topic, t.Note)
	}

	b.WriteString("\nBUILTINS (by category)\n")
	cat := ""
	for _, bi := range g.Builtins {
		if bi.Category != cat {
			cat = bi.Category
			fmt.Fprintf(&b, "  [%s]\n", cat)
		}
		fmt.Fprintf(&b, "    %s %s — %s\n", bi.Name, bi.Sig, bi.Summary)
	}

	b.WriteString("\nIDIOMS\n")
	for _, id := range g.Idioms {
		fmt.Fprintf(&b, "  # %s\n", id.Name)
		for _, line := range strings.Split(id.Code, "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}

	b.WriteString("GOTCHAS\n")
	for _, n := range g.Gotchas {
		fmt.Fprintf(&b, "  %s: %s\n", n.Topic, n.Note)
	}
	return b.String()
}
