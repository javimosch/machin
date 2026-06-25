package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// machinVersion is the single version string for the toolchain. Bump it when
// cutting a release (alongside README badge / SPEC / CHANGELOG).
const machinVersion = "0.58.0"

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

// builtinNames is the set of builtin function names (from the catalog), memoized.
// Used to reject a user function that would silently shadow a builtin.
var builtinNames map[string]bool

func isBuiltinName(n string) bool {
	if builtinNames == nil {
		builtinNames = map[string]bool{}
		for _, b := range machinGuide().Builtins {
			builtinNames[b.Name] = true
		}
	}
	return builtinNames[n]
}

func machinGuide() guideCatalog {
	return guideCatalog{
		Version: machinVersion,
		Schema:  "machin.guide/v1",
		Tagline: "Go-flavored, type-inferred, machine-first language; MFL compiles through C to a single native binary. Plain-text source, one declaration per line.",
		Keywords: []string{
			"func", "return", "if", "else", "while", "for", "range", "break", "continue",
			"select", "go", "chan", "make", "map", "struct", "type", "var", "arena",
			"extern", "export", "true", "false", "nil",
		},
		Types: []guideNote{
			{"int", "64-bit signed"},
			{"float", "double"},
			{"bool", "true/false"},
			{"string", "immutable UTF-8 bytes; zero value \"\""},
			{"bytes", "NUL-safe binary buffer (ptr+len); from bytes()/from_hex(); inspect with len/byte_at/to_hex. For binary protocols & crypto (strings truncate at NUL)"},
			{"[]T", "slice (append to grow); for i, v := range. T can be a struct, another slice, or `func` (`[]func{}` — a slice of closures, for dispatch tables / effect lists)"},
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
			{"raw_mode", "(int) -> int", "put the terminal in cbreak/no-echo mode (1) or restore it (0); pair them and restore before exit (for TUIs/games)", "io"},
			{"read_key", "() -> string", "non-blocking single-key read: a 1-char string, or \"\" if no key is waiting (needs raw_mode for live input)", "io"},
			{"read_file", "(string) -> string", "read a whole file (\"\" on error)", "io"},
			{"read_file_bytes", "(string) -> bytes", "read a whole file's raw bytes, NUL-safe (empty on error) — for binary assets", "io"},
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
			{"time_format_utc", "(int, string) -> string", "like time_format but in UTC (gmtime) — the form .ics / RFC-3339 stamps want", "time"},
			{"time_make", "(int, int, int, int, int, int) -> int", "build a unix timestamp from local calendar fields (year,month,day,hour,min,sec); inverse of time_fields, normalizes overflow", "time"},
			// convert
			{"str", "(int|float|bool|string) -> string", "format a value: number, bool (\"true\"/\"false\"), or string (identity)", "convert"},
			{"int", "(number) -> int", "truncate to int", "convert"},
			{"float", "(number) -> float", "int -> float (identity on float). MFL has no implicit int->float, so a concrete int (fn return, byte_at, len, ...) needs this to enter float math", "convert"},
			{"parse_int", "(string) -> int", "parse an integer (0 if non-numeric)", "convert"},
			// math (libm, linked -lm only when used; numeric in, float out)
			{"sqrt", "(number) -> float", "square root", "math"},
			{"cbrt", "(number) -> float", "cube root", "math"},
			{"pow", "(number, number) -> float", "x raised to y", "math"},
			{"exp", "(number) -> float", "e^x", "math"},
			{"log", "(number) -> float", "natural log; also log2, log10", "math"},
			{"sin", "(number) -> float", "sine (radians); also cos, tan", "math"},
			{"asin", "(number) -> float", "arcsine; also acos, atan", "math"},
			{"atan2", "(number, number) -> float", "atan(y/x) with quadrant (y, x)", "math"},
			{"hypot", "(number, number) -> float", "sqrt(x*x+y*y) without overflow", "math"},
			{"floor", "(number) -> float", "round toward -inf; also ceil, round, trunc", "math"},
			{"abs", "(number) -> float", "absolute value (float; fabs)", "math"},
			{"fmod", "(number, number) -> float", "floating-point remainder of x/y", "math"},
			{"pi", "() -> float", "the constant pi", "math"},
			{"noise2", "(number, number) -> float", "Perlin gradient noise (2D), deterministic, ~[-1,1], smooth. Layer it (fbm) by summing octaves at scaled freq/amp", "math"},
			{"noise3", "(number, number, number) -> float", "Perlin gradient noise (3D) — animate 2D noise over time, or volumetric", "math"},
			// raw memory (pointers are ints) — build C buffers/structs for the FFI
			{"alloc", "(int) -> int", "allocate n zeroed bytes; returns a pointer (as int). For C buffers/structs to hand to an extern fn", "memory"},
			{"free", "(int) ->", "free a pointer returned by alloc", "memory"},
			{"poke_f32", "(int, int, number) ->", "write a 32-bit float at ptr+byteoffset", "memory"},
			{"poke_i32", "(int, int, int) ->", "write a 32-bit int at ptr+byteoffset (also poke_u8, poke_u16)", "memory"},
			{"poke_ptr", "(int, int, int) ->", "write an 8-byte pointer value at ptr+byteoffset (e.g. a buffer into a struct field)", "memory"},
			{"peek_f32", "(int, int) -> float", "read a 32-bit float at ptr+byteoffset", "memory"},
			{"peek_i32", "(int, int) -> int", "read a 32-bit int at ptr+byteoffset", "memory"},
			{"ptr_str", "(int) -> string", "read a NUL-terminated string from a raw pointer into an MFL string — the host->wasm string direction (host writes UTF-8+NUL into wasm memory at an alloc'd ptr, passes it to an export). Pairs with alloc/free.", "memory"},
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
			{"url_encode", "(string) -> string", "percent-encode for URLs (RFC 3986; keeps A-Za-z0-9-._~, space -> %20)", "string"},
			{"url_decode", "(string) -> string", "percent-decode a URL component (lenient: + -> space, bad %XX passes through)", "string"},
			{"sha256", "(string) -> string", "SHA-256 of text, lowercase hex", "crypto"},
			{"hmac_sha256", "(string, string) -> string", "HMAC-SHA256(key, message), lowercase hex (webhook signatures)", "crypto"},
			// bytes (a NUL-safe binary buffer — the type strings can't be; for binary protocols/crypto)
			{"bytes", "(string) -> bytes", "make a bytes value from a string's raw bytes", "bytes"},
			{"bytes_str", "(bytes) -> string", "bytes -> string (NUL-terminated; truncates at an embedded 0)", "bytes"},
			{"to_hex", "(bytes) -> string", "lowercase hex of a bytes value", "bytes"},
			{"from_hex", "(string) -> bytes", "parse hex -> bytes (skips non-hex chars)", "bytes"},
			{"byte_at", "(bytes, int) -> int", "byte value 0-255 at an index (-1 if out of range)", "bytes"},
			{"bytes_sub", "(bytes, int, int) -> bytes", "sub-range [start, end) of a bytes value", "bytes"},
			{"bytes_concat", "(bytes, bytes) -> bytes", "concatenate two bytes values", "bytes"},
			// crypto over bytes (OpenSSL libcrypto, linked only when used)
			{"rand_bytes", "(int) -> bytes", "n cryptographically-random bytes (CSPRNG)", "crypto"},
			{"sha256_bytes", "(bytes) -> bytes", "SHA-256 of binary -> 32-byte digest (binary-safe, unlike sha256)", "crypto"},
			{"hmac_sha256_bytes", "(bytes, bytes) -> bytes", "HMAC-SHA256(key, msg) -> 32 bytes (binary-safe)", "crypto"},
			{"hkdf_sha256", "(bytes, bytes, bytes, int) -> bytes", "HKDF-SHA256(ikm, salt, info, length) -> length bytes", "crypto"},
			{"x25519_pub", "(bytes) -> bytes", "X25519 public key from a 32-byte private key", "crypto"},
			{"x25519_shared", "(bytes, bytes) -> bytes", "X25519 ECDH shared secret (my private, their public) -> 32 bytes", "crypto"},
			{"ed25519_pub", "(bytes) -> bytes", "Ed25519 public key from a 32-byte seed", "crypto"},
			{"ed25519_sign", "(bytes, bytes) -> bytes", "Ed25519 sign (seed, msg) -> 64-byte signature", "crypto"},
			{"ed25519_verify", "(bytes, bytes, bytes) -> bool", "Ed25519 verify (pub, msg, sig)", "crypto"},
			{"aes_gcm_encrypt", "(bytes, bytes, bytes, bytes) -> bytes", "AES-GCM (key, iv, plaintext, aad) -> ciphertext||16-byte tag (key 16 or 32)", "crypto"},
			{"aes_gcm_decrypt", "(bytes, bytes, bytes, bytes) -> bytes", "AES-GCM decrypt (key, iv, ct||tag, aad) -> plaintext (empty bytes on auth failure)", "crypto"},
			{"aes_cbc_encrypt", "(bytes, bytes, bytes) -> bytes", "AES-CBC encrypt (key, iv, plaintext), PKCS#7 padded", "crypto"},
			{"aes_cbc_decrypt", "(bytes, bytes, bytes) -> bytes", "AES-CBC decrypt (key, iv, ciphertext) -> plaintext (empty on bad padding)", "crypto"},
			{"xeddsa_sign", "(bytes, bytes, bytes) -> bytes", "XEdDSA sign over Curve25519 (priv32, msg, random64) -> 64-byte sig (Signal/WhatsApp identity sigs); needs libsodium", "crypto"},
			{"xeddsa_verify", "(bytes, bytes, bytes) -> bool", "XEdDSA verify (curve25519 pub32, msg, sig64); needs libsodium", "crypto"},
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
			{"write_bytes", "(int, bytes) -> int", "write raw bytes to an fd, NUL-safe — for binary HTTP responses", "net"},
			{"close", "(int|chan) ->", "close an fd, or a channel (by argument type)", "net"},
			{"https_get", "(string) -> string", "GET over TLS (or plain http:// URLs); body (\"\" on error)", "net"},
			{"https_post", "(string, string) -> string", "POST (JSON body) over TLS (or plain http://); body", "net"},
			{"http_get", "(string) -> (int, string, string)", "GET (http:// or https://) -> (status, body, err); err \"\"/dns/connect/tls. MULTI-ASSIGN ONLY", "net"},
			{"http_request", "(string, string, []string, string) -> (int, string, string)", "auth'd HTTP(S): (method, url [http/https], header lines like \"Authorization: Bearer x\", body) -> (status, body, err). MULTI-ASSIGN ONLY", "net"},
			// websocket
			{"wss_open", "(string) -> int", "open a wss:// WebSocket -> handle (0 on fail)", "ws"},
			{"wss_send", "(int, string) -> int", "send a text message", "ws"},
			{"wss_recv", "(int) -> string", "next message (blocks; \"\" on close; auto ping/pong)", "ws"},
			{"wss_send_bin", "(int, bytes) -> int", "send a binary message (opcode 0x2) — NUL-safe, for binary protocols", "ws"},
			{"wss_recv_bin", "(int) -> bytes", "next message as bytes (blocks; empty bytes on close; NUL-safe)", "ws"},
			{"wss_close", "(int) -> int", "send close and tear down", "ws"},
		},
		Idioms: []guideIdiom{
			{"hello", `func main() { println("hello") }`},
			{"bytes", `func main() { b := from_hex("deadbeef")  b = bytes_concat(b, bytes("!"))  println(to_hex(b) + " len=" + str(len(b)) + " b0=" + str(byte_at(b, 0))) }`},
			{"bitwise", `func main() { x := 0xa5  println(str(x >> 4 & 0x0f) + " " + str(x | 0x100) + " " + str(^x & 0xff)) }`},
			{"terminal-input", `func main() { raw_mode(1)  esc := bytes_str(from_hex("1b"))  k := read_key()  if k == "q" { print(esc + "[2J") }  raw_mode(0) }`},
			{"fbm-noise", `func fbm(x, y) (s) { s = 0.0  amp := 1.0  fr := 1.0  o := 0  while o < 5 { s = s + amp * noise2(x * fr, y * fr)  amp = amp * 0.5  fr = fr * 2.0  o = o + 1 } }
func main() { println(str(fbm(1.5, 2.5))) }`},
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
			{"int-float-no-implicit", "There is NO implicit int->float. Only a flexible numeric LITERAL (e.g. 5) promotes against a float; a CONCRETE int — a fn return, byte_at, len, a typed param, or an int-slice element — is a hard `int vs float` mismatch with a float. Wrap it: float(byte_at(b,0)) / 2.0. (int() goes the other way.) This also applies to f32/f64 struct fields in FFI cstructs."},
			{"ffi-opaque-handle", "For a by-value C struct that contains pointers (raylib Sound/Music/Font/Texture with internals, FILE wrappers, ...), declare an OPAQUE cstruct with an empty body: `cstruct Sound {}`. machin holds the real C struct by value and passes it back to fns without naming its fields — receive it from a fn, store it (incl. []Sound), pass it on; no construct or .field. (A single pointer is simpler: the `ptr` FFI type, held as an int.)"},
			{"ffi-nested-cstruct", "A cstruct field may be ANOTHER cstruct (by-value struct of structs) — declare the inner one first. e.g. `cstruct Vector3 { x f32 y f32 z f32 }` then `cstruct Camera3D { position Vector3 target Vector3 up Vector3 fovy f32 projection i32 }`; construct with nested literals `Camera3D{Vector3{0,10,10}, Vector3{0,0,0}, Vector3{0,1,0}, 45.0, 0}`. Required for 3D (BeginMode3D) and 2D cameras. (Camera/orbit trig: machin has native sin/cos/sqrt/pi etc. — see the `math` builtins.)"},
			{"ffi-raw-buffers", "Hand a C API raw buffers + structs via raw memory (pointers are ints): `p := alloc(nbytes)` (zeroed), `poke_f32/poke_i32/poke_u8/poke_ptr(p, byteOffset, v)`, `peek_f32/peek_i32`, `free(p)`. Struct pointer params: a cstruct FIELD may be `ptr` (a pointer, held as int) so you declare a struct like raylib Mesh `{vertexCount i32 vertices ptr colors ptr vaoId u32 vboId ptr}` and the C compiler lays it out (no offsets). Pass a cstruct by pointer with writeback via an INOUT param `Name*` (`fn UploadMesh(Mesh*, bool)` — arg must be a variable). Pass a raw pointer by value with `ptr` (becomes void*->any T*); deref a raw pointer to a by-value struct with `*Name`. GPU mesh: build vertex/color arrays with alloc/poke, `mesh := Mesh{vcount, vcount/3, vbuf, cbuf, 0, 0}`, `UploadMesh(mesh, false)`, `LoadModelFromMesh(mesh)`. (see machin-game-demo-planet)"},
			{"eval-order", "Operands and arguments evaluate left-to-right (as in Go), including side effects: `f() + g()` runs f() before g(). Holds for binary ops, call args, slice/struct literals, and multi-return lists."},
			{"composite-literal", "T{...} literals need T to be a known struct type at parse time. `machin encode` registers all `type` decls first, so this just works in normal builds."},
			{"stdout-buffering", "libc fully buffers stdout when it's a pipe; a streaming program must call flush() after a write to appear promptly downstream. A TTY is line-buffered."},
			{"channels-cross-goroutine", "Values sent over a channel are deep-copied across the goroutine/arena boundary (strings fast; slices/maps/structs via JSON), so they survive the sender goroutine. Channels of closures/funcs are not deep-copied."},
			{"select-closed", "A closed channel makes its select receive case ready, firing repeatedly (with ok==false if you wrote `case v, ok := <-ch:`). Detect close and stop selecting on it."},
			{"no-tls-without-https", "There is no raw TLS socket; use https_get/https_post (REST) and wss_* (WebSocket). Plain dial/listen are TCP without TLS."},
			{"floats-over-chan-json", "A slice/map channel element round-trips through JSON, which formats floats with %g (not bit-exact for pathological doubles)."},
			{"memory", "Per-goroutine arena, reclaimed in bulk when the goroutine returns; wrap a hot allocating loop in `arena { ... }` to keep peak memory flat. Build with --safe for bounds/overflow/div-zero checks."},
			{"wasm-target", "`machin build app.mfl --target wasm` compiles to a WebAssembly reactor module (needs `zig` as the C->wasm compiler; override with ZIG=). Mark host-callable functions `export func name(...)` — they become wasm exports under their clean name (and are reachability roots, so a wasm module needs no main). A headerless `extern \"env\" { fn dom_set(string) }` becomes a wasm IMPORT the JS host supplies (the `extern \"<lib>\"` name is the import module). Marshaling host-side: machin ints are i64 -> pass/return BigInt; strings are a pointer into the exported `memory` (decode NUL-terminated UTF-8). App state can live in machin via package globals (`var count = 0`), which persist across export calls. See docs/NORTH-STAR-WEB.md."},
			{"package-globals", "A top-level `var name = expr` is a package GLOBAL: mutable, shared by every function, type inferred from the initializer + uses. It PERSISTS across calls (unlike a local), so a wasm export can hold state between host calls (`var count = 0` + `export func bump(d){count=count+d}`). `=` assigns the global; `:=` makes a local (and may shadow it). Globals work everywhere incl. closures (a captured global is referenced directly), and for any type incl. make-maps and slices. Init runs before main / at wasm `_initialize`."},
			{"lambda-and-builtin-names", "Two rules when defining functions/closures: (1) a lambda (`func(){...}`) has NO named returns — `func() (s) { s = x }` does NOT parse; use `func() { return x }`. (2) A user function may NOT be named like a builtin (`flush`, `len`, `str`, `keys`, `contains`, ...) — it is a compile error (the builtin would win at call sites, silently ignoring your function). Pick another name. (An `extern` MAY shadow a builtin — that's intentional for FFI.)"},
			{"reactive-runtime", "`framework/reactive.src` is a fine-grained reactive runtime (signals + a patch list) for wasm UIs. `signal(v)`/`get`/`set`; `computed(func(){ return ... })` is a memoized derived signal; `bind(slot, func(){ return str(...) })` patches a DOM text slot on change; `each(container, func(){ get(ver)  return csv(ids) }, func(k){ return html })` does keyed list reconciliation (keys as a CSV string; emits insert/remove/order). Only reactions that read a changed signal recompute, and only changed text/keys patch. TEMPLATING (v0.55.0): declare markup+bindings together — `slot(name, compute)` and `list(name, keys, item)` return markup AND queue a reaction; `mount(root, html)` sets the root HTML once then flushes them (so `mount(\"app\", \"<h1>x</h1>\" + slot(\"n\", fn) + list(\"items\", keys, item))` is the whole component). ISOMORPHIC (v0.56.0): `slot` embeds its initial value, and `hydrate(html)` attaches bindings to an SSR server's existing DOM (by data-s names) instead of re-rendering — so one component SSRs on the server and hydrates in the browser (see boilerplate-cli-ui-machin-isomorphic). Host supplies dom_mount/dom_patch/list_insert/list_remove/list_order. Drove `[]func` (v0.53.0)."},
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
