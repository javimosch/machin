package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// The domain how-to guides, embedded into the binary so an agent that installed via
// curl|sh (no repo checkout) can read them offline: `machin guide --skill web|gamedev`.
//
//go:embed skills/machin-start/SKILL.md
var skillStart string

//go:embed skills/machin-web/SKILL.md
var skillWeb string

//go:embed skills/machin-gamedev/SKILL.md
var skillGamedev string

//go:embed skills/machin-backend/SKILL.md
var skillBackend string

//go:embed skills/machin-deploy/SKILL.md
var skillDeploy string

// machinVersion is the single version string for the toolchain. Bump it when
// cutting a release (alongside README badge / SPEC / CHANGELOG).
const machinVersion = "0.113.0"

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

type guideDomain struct {
	Name    string `json:"name"`
	Skill   string `json:"skill"` // run `machin guide --skill <this>` for the full how-to ("" = none yet)
	Howto   string `json:"howto"` // where the how-to lives
	Summary string `json:"summary"`
}

// guideBenchmark is one measured (not claimed) comparison — see bench/ in the
// repo. Reproduce is the exact command an agent (or a skeptical human) runs to
// re-derive the number itself, not just a pointer to trust.
type guideBenchmark struct {
	Axis      string `json:"axis"`
	Result    string `json:"result"`
	Reproduce string `json:"reproduce"`
}

// guideProof is the "why trust these claims" section — deliberately structured
// data, not prose, so another agent evaluating machin (for itself or on behalf
// of its principal) can read and relay it, the same way it reads Builtins or
// Idioms. Every number here is reproducible from the repo (see Reproduce on
// each benchmark), not asserted.
type guideProof struct {
	SelfHosting string           `json:"selfHosting"`
	Benchmarks  []guideBenchmark `json:"benchmarks"`
}

// guideCommand is one CLI subcommand — so an agent can enumerate the whole tool
// surface, not just the language.
type guideCommand struct {
	Name    string `json:"name"`
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
}

type guideCatalog struct {
	Version  string         `json:"version"`
	Schema   string         `json:"schema"`
	Tagline  string         `json:"tagline"`
	Keywords []string       `json:"keywords"`
	Commands []guideCommand `json:"commands"`
	Domains  []guideDomain  `json:"domains"`
	Proof    guideProof     `json:"proof"`
	Types    []guideNote    `json:"types"`
	Builtins []guideBuiltin `json:"builtins"`
	Idioms   []guideIdiom   `json:"idioms"`
	Gotchas  []guideNote    `json:"gotchas"`
}

// guideDomains routes an agent from `machin guide` (the language) to the per-domain
// how-to. Embedded skills are printable offline via `machin guide --skill <name>`.
var guideDomains = []guideDomain{
	{"start", "start", "machin guide --skill start", "START HERE — decide whether machin fits the task and bootstrap it fast: when it wins (with measured numbers) vs Go/Node/Python, when NOT to use it, and a zero→running→shipped quickstart (install → a REST+SQLite service → a 92.9 kB FROM-scratch image). Routes to the domain skills below."},
	{"web", "web", "machin guide --skill web", "Full-stack web: a native HTTP server (machweb), SSR, a reactive WebAssembly UI (signals + keyed lists), a client-side router, cookies + signed sessions, and OAuth2/OIDC SSO — one language both ends, no Node/bundler."},
	{"gamedev", "gamedev", "machin guide --skill gamedev", "Native games: terminal TUI (raw_mode/read_key + ANSI) and raylib GUI/audio/3D through the C FFI — sprites, sound, 3D cameras, GPU meshes (pointer/array FFI), instancing, shaders, procedural worlds (noise)."},
	{"backend", "backend", "machin guide --skill backend", "Single-binary backends: HTTP/JSON APIs, pure-MFL drivers for SQLite, PostgreSQL (SCRAM), MySQL/MariaDB, Redis, and MongoDB (all connection-pooled, uniform JSON rows → parse([]T{})), signed sessions, OAuth2/OIDC SSO, agent-first CLIs, and daemons."},
	{"deploy", "deploy", "machin guide --skill deploy", "Ship a machin web app to production: run it behind a reverse proxy (nginx/Caddy/Traefik/Cloudflare) correctly and safely — proxy-awareness (X-Forwarded-Proto/For → scheme/client_ip/base_url, Secure cookies), hardening (body cap, read timeout, access logs), a systemd unit, and a slim Docker image."},
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
		Schema:  "machin.guide/v3",
		Tagline: "Go-flavored, type-inferred, machine-first language; MFL compiles through C to a single native binary. Plain-text source, one declaration per line.",
		Keywords: []string{
			"func", "return", "if", "else", "while", "for", "range", "break", "continue",
			"select", "go", "chan", "make", "map", "struct", "type", "var", "arena",
			"extern", "export", "true", "false", "nil",
			// extern-block-only: `fn` declares a foreign function (distinct from `func`),
			// `cstruct` declares a C struct layout, and header/link/cflags are directives.
			"fn", "cstruct", "header", "link", "cflags",
		},
		Commands: []guideCommand{
			{"run", "machin run <file.mfl> [--safe] [--record <trace>|--replay <trace>] [--verify]", "compile to native and execute in one step. --record <trace> writes a deterministic-replay trace (the goroutine schedule + I/O log); --replay <trace> re-executes it byte-for-byte, feeding the recorded schedule and I/O instead of the real clock/stdin. SOUND because machin proves the program race-free — replay records the channel-op order, not memory accesses. A program that uses FFI leaves the determinism boundary, so its trace is flagged best-effort (replay warns, and --verify never certifies it FAITHFUL). Everything else that varies is captured — the channel schedule, concurrent prints, time, stdin, rand_bytes, file reads, raw fd sockets, and the HTTP/TLS/WebSocket reads (a recorded API response replays offline) — and `select` is gated, so those stay faithful and self-contained. (Interactive read_key/raw_mode TTY input is not yet captured.) DEADLOCK DETECTION is always on: machin channels are unbounded (send never blocks), so a deadlock is every live goroutine parked in a receive with no one left to send — the runtime detects that (quiescence: all parked + no channel send/close across a short window) and exits 2 instead of hanging forever. The report is a causal wait-cycle — each blocked goroutine (by its stable id path) and the channel id it can never receive from — so you see the cycle, not just a count; `machin run --json` (or MFL_RR_JSON) emits it as {\"deadlock\":true,\"goroutines\":N,\"blocked\":[{\"goroutine\":\"0\",\"recvOnChannel\":1},...]} an agent reads instead of reproduces. Correct concurrent programs are never flagged. Covers a blocking receive and a select-spin (a no-default select whose cases can never fire busy-polls, counted as parked so an all-parked program is caught). Blocking READS (socket/stdin) are NOT counted by default — an idle server waiting on accept/read must not be mistaken for a deadlock — but `machin run --deadlock-strict` (env MFL_DL_STRICT) opts a program in: a read that stalls with no progress across the window then counts as a park too, so a batch job/test/tool stuck on I/O is reported instead of hanging (an active reader still makes progress and is never flagged). Not covered even in strict: an infinite compute loop (undecidable) or a blocking FFI call."},
			{"replay", "machin replay <trace> [--json] [--verify] [--print <var>[,<var>...]]", "re-execute a recorded trace (from `machin run --record`). The trace embeds its program path + build flags, so you replay without re-naming the source; a recorded CRASH reproduces exactly. --json turns a crash into a structured causal report {panic, goroutine, scheduleOp, causalChain:[goroutine-ids of the channel ops that led here]} an agent reads instead of reproduces. --verify certifies the replay stayed faithful (see `run`). --print <var> is the value-query debugger: it rebuilds with a probe after each assignment to <var> and prints that variable's value history (deterministic, since replay is) as `probe <goroutine> <var> = <value> @op<n>` on stderr — the last line before a panic is the variable's value at the crash. Scalars/strings only; a normal build emits no probes."},
			{"build", "machin build <file.mfl> [-o out] [--target wasm] [--static] [--emit-c] [--safe] [--race-safe]", "compile to a native binary (or a wasm module with --target wasm; mark exports with `export func`). --static bundles SQLite for a FROM-scratch binary (pair with CC=musl-gcc); --emit-c prints the generated C and stops; --safe inserts bounds/div-zero/overflow checks; --race-safe refuses to build if a data race is inferred (see `check`)."},
			{"encode", "machin encode <src...>", "mint canonical MFL (one declaration/line) from loose Go-like .src; multiple files concatenate (compose a framework with an app); framework/*.src resolve from the binary."},
			{"check", "machin check [--json] <src...>|--stdin", "lex/parse/typecheck + inferred data-race analysis + advisory falsification — no cc, milliseconds — the fast write→check→fix loop. --json returns {ok, errorCount, diagnostics:[{severity, phase, code, message, decl, line, snippet}], warnings:[...]}, exit 0 iff no errors. Branch on the stable `code` (type-mismatch/undefined-name/undefined-field/arity-mismatch/parse-*/no-main/unsupported-construct/...), NOT the message text; `decl` is the function to fix. The `race` phase reports inferred data races with NO annotations (the guarantee Rust needs Send/Sync for): RACE001 write/write, RACE002 read/write, RACE004 use-after-move (value used after `ch <- v` moved it), each with a counterexample in `message`. The `falsify` phase (in `warnings`, advisory — never fails check/build) reports bounded bug counterexamples: FALS001 index out of range, FALS002 divide/modulo by zero, FALS010 postcondition (`ensures`) violated, each with the exact failing input. The `deadlock` phase (also advisory `warnings`) reports DL001: a receive on a channel that nothing ever sends to or closes — a guaranteed deadlock, found at COMPILE time (machin channels are unbounded, so such a receive blocks in every schedule). False-positive-free: a channel is treated as fed unless the analysis PROVES it's never fed (following the channel through goroutines/calls via a per-function fixpoint; a use it can't account for is conservatively a feed), so a clean result is not a proof of deadlock-freedom — the runtime detector catches the rest. The `arena` phase (also advisory `warnings`) reports two codes. ARENA001: a value ALLOCATED INSIDE an `arena { }` block that escapes to a location outliving the block (assigned to an outer variable / named return, stored into an outer slice/struct) — after the block's bulk memory reclamation that reference DANGLES. This includes a CLOSURE that captures an arena-tainted variable and then escapes: the capture is read inside the block but stored in the closure's block-outliving environment, so calling it after reclamation reads freed memory (a captureless closure, or one capturing only a parameter, is safe). ARENA002: ANY `return` inside an arena block — the generated code returns before the block's cleanup runs, so it leaks the block's allocations AND leaves the current-arena pointer dangling into the freed stack frame (later allocations corrupt); this fires regardless of the returned value, so move the `return` after the arena block. Unlike the falsify/deadlock passes this is SOUND + conservative (like the race pass): a clean result PROVES nothing escapes, so the arena's reclamation is safe — memory safety without a borrow checker or lifetime annotations, all inferred. Taint tracks provenance (only values allocated inside the block; parameters/outer variables/globals are pre-existing and safe) with a result-kind gate (a scalar like `len(s)` never carries arena memory, so the `total = total + len(s)` accumulator stays clean); channel sends are not escapes (the runtime deep-copies values crossing a channel). Provenance is INTERPROCEDURAL: a per-function return summary (computed to a call-graph fixpoint) records whether a function returns freshly-allocated heap or passes a parameter through, so a pass-through helper (r = s) called on a pre-existing value inside the block does NOT report an escape, while a helper that allocates (r = a concatenation of s) still does. Reserve a full `build` for when you actually need the binary."},
			{"certify", "machin certify [--json] <src...>", "translation validation — the self-certifying compiler. Every other guarantee (race-freedom, falsify, replay) assumes the compiler faithfully implemented your source; this checks that assumption per build. It runs each function through machin's own concrete interpreter (the SOURCE semantics) AND the actually-compiled binary (the CODEGEN semantics) over the same bounded input space (the falsifier's domain: ints [0,1,-1,2,3], slice len ≤ 3) and confirms they produce identical results — or reports the EXACT input where the compiler diverged (a TRV001 miscompilation, with source-value vs compiled-value). Per-function verdict: `certified` (finite domain, whole space agreed), `certified-bounded` (agreed up to the bounds), `partial` (agreed on the modeled inputs; some not modeled), `unknown` (not validatable — e.g. a non-scalar return in this version), `miscompiled`. Honest, falsifier-style: a clean result means 'no miscompilation found within bounds', NEVER 'the compiler is proven correct'; unsound-complete, so every divergence reported is real. Exits non-zero on a miscompilation. Only instantiated (actually-compiled) functions are validated. Validates int/float/bool/string returns, structs, and []int (built via literal + append) — via the json() canonical form; a return json() can't serialize (closure/channel/map) stays unknown."},
			{"falsify", "machin falsify [--json] [--repro <dir>] [--strict] <src...>|--stdin", "bounded bug-finder: enumerates small concrete inputs and returns the EXACT input that breaks a function (FALS001 index out of range, FALS002 divide/modulo by zero, FALS010 postcondition violated); reaches through calls (interprocedural) and struct fields. Design-by-contract: a function may carry declarative `requires <expr>` / `ensures <expr>` clauses on its signature (after the return list, before the body) — `requires b != 0` is a precondition that FILTERS the input domain (inputs failing it are not counterexamples, so it suppresses a would-be FALS002); `ensures r >= 0` (references named returns) is a postcondition, and an input satisfying every `requires` that makes an `ensures` false is a FALS010. Unsound-complete — finds bugs, never proves absence — so a clean result means 'no bug within bounds', not 'correct'; the `--json` envelope {ok, counterexamples, findings, coverage, functions:[{fn, verdict, tried}], bounds:{sliceLenMax, intDomain, callDepth}} NEVER claims proved. False-positive-free by construction. --repro <dir> writes one runnable .mfl per finding (FALS001/002 panic under --safe at the predicted trap; a regression test). --strict exits non-zero on any counterexample (for CI; advisory by default). --prove switches from the sparse bug-finding sample to a DENSE, fully-covered bounded space: exhausting it clean earns an honest `proved` verdict (finite/bool-only domains — a total proof) or `proved-bounded` (int/[]int — proof only up to the reported `bounds`, NEVER 'correct'); float/string params (infinite domains) and any unmodeled path block a proof (verdict stays clean/unknown). --prove also finds bugs the sparse sample misses."},
			{"equiv", "machin equiv <f> <g> [--json] <src...>", "bounded equivalence oracle — prove two functions are observationally equivalent, or return the exact input where they diverge. Runs BOTH functions through machin's own interpreter (the same evaluator certify and the falsifier use) over f's bounded input domain (the falsifier's: ints [0,1,-1,2,3], slice len ≤ 3) and compares canonical outputs. This is the verification step behind provable superoptimization (accept a faster rewrite only when it's proven equivalent) AND a standalone 'did my refactor change behavior?' tool: `machin equiv sum_loop sum_formula prog.mfl` proves a loop equals its closed form, or catches a wrong rewrite as `DIVERGES: sum_loop(1)=1 but sum_bad(1)=0`. Verdicts: `equivalent` (finite domain e.g. bool-only — whole space agreed, a total result), `equivalent-bounded` (agreed on every checked input up to the int/slice bounds — NOT a total proof), `diverges` (a hard counterexample: the exact input + both values; exits non-zero), `inconclusive` (arity mismatch, unbounded param domain, or a body the interpreter can't model — never a silent 'equivalent'). f and g must take the same number of params. Honest/falsifier-style: a clean `equivalent-bounded` means 'no divergence found within the bounds', never 'proven equal for all inputs'. --json emits {f, g, verdict, tried, compared, input?, fValue?, gValue?}."},
			{"optimize", "machin optimize [--json] <src...>", "provable superoptimization — propose faster equivalent code and keep ONLY the rewrites the equivalence oracle (see `equiv`) proves. For every instantiated, bounded-domain function it tries semantics-preserving rewrites — constant folding (a + 2*3 -> a + 6), algebraic identity elimination (a*1 + 0 -> a, x - x -> 0, x*0 -> 0 for pure x), strength reduction (x*8 -> x<<3) — and GATES each through the bounded equivalence check before accepting it. What makes it 'provable': unlike a classic optimizer whose rules you must trust, each applied rewrite carries an equivalence verdict, and a speculative-but-unsound rule is CAUGHT, not shipped. E.g. lowering signed x/2 to x>>1 is refuted with the exact input — `halve(-1) would change from 0 to -1` — and dropped. Per function it reports each pass (✓ proven equivalent[-bounded] / ✗ rejected-by-oracle with the counterexample / · skipped-not-certifiable), the rendered optimized function, and a static COST estimate `before → after (−N%)` quantifying the rewrite. Cost = latency-weighted operation count (mul=4, div/mod=5, shift/add/bitwise/cmp=1; loop bodies ×10), the deterministic, backend-independent proxy superoptimizers rank equivalent sequences by — NOT a wall-clock claim. It is a cost model on purpose: `machin build` compiles through `cc -O2`, so the C backend already applies these same peephole rewrites and micro-benchmarking them would measure noise; the model ranks the rewrites honestly, and the large wall-clock wins come from algorithmic rewrites. Honest/bounded like the rest of the evidence tools: `equivalent-bounded` means agreed over the falsifier's bounded domain (ints [0,1,-1,2,3], slice len ≤ 3), not a proof for all inputs. --json emits {funcs:[{fn, passes:[{rule, verdict, detail}], optimized, note, costBefore, costAfter}], applied, rejected, costBefore, costAfter}. Rewrites only arithmetic/control-flow bodies (not closures/channels); functions with an unbounded param domain are noted, not optimized."},
			{"superopt", "machin superopt <fn> [--json] <src...>", "search-driven rewrite DISCOVERY — where `optimize` applies a fixed catalog of peephole rules, this SEARCHES for a cheaper equivalent of <fn> from scratch via bottom-up enumerative synthesis over an arithmetic grammar (params, small constants, + - * / % and, for one-parameter functions, shifts << >>), enumerated in COST order and pruned by observational equivalence (one representative per distinct behavior — the first, hence cheapest, in cost order). It finds rewrites the fixed rules cannot: strength reduction (a*8 → a<<3), reassociation/factoring (a*3+a*5 → a<<3), and even LOOP ELIMINATION — a constant-count loop `for i<3 { s=s+a }` is discovered to equal `a+(a+a)`, cost −90%. The result is the provably cost-minimal form within the grammar (e.g. tripling is `a+(a+a)`, cost 2, not the costlier `a*3`). Two-parameter functions use a leaner grammar (no shifts) so the squared search space stays tractable. Int→int functions with 1–2 params (others declined with a note). Two overfitting guards, because synthesis-from-examples can match the samples yet differ elsewhere: (1) the sample domain is DENSE (ints −6..6, or −4..4 per param for 2 params), far more constraining than a few points — this is what correctly REFUSES sum-to-n → n(n+1)/2, which disagrees on negatives (the loop returns 0 for n≤0); (2) the winning candidate is re-checked over a WIDER domain (−20..20) and discarded on any disagreement, then certified through the same equivalence oracle as `equiv`/`optimize`. Honest/bounded: the verdict is `equivalent-bounded` (agrees over the checked inputs), NEVER 'equivalent for all inputs' — a synthesized rewrite is an evidence-backed SUGGESTION to review, not a structural proof, and the output says so. Only a STRICTLY cheaper (by the cost model) equivalent is reported. --json emits {fn, found, expr, optimized, verdict, costBefore, costAfter, explored, distinct, widePoints, note}."},
			{"test", "machin test [--json] <src...>", "run MFL test files: composes framework/test.src ahead of the given sources (same multi-file compose as `encode` — pass a framework module alongside its test file to test it), builds+runs the result as one program, reports the pass/fail tally. framework/test.src provides `assert(cond, msg)`, `assert_eq_int(got, want, name)`, `assert_eq_str(got, want, name)`, and `test_summary()` (call last in main() — prints the tally, exits 1 on any failure). --json returns {ok, passed, failed, files}. One composed program per invocation — run it once per test suite, not once for a whole tree."},
			{"pack", "machin pack <file.mfl>", "emit the dense base64 distribution form (`run` reads either plain or packed)"},
			{"guide", "machin guide [--text] [--skill <name>]", "this version-exact catalog as JSON (--text for prose); --skill <start|web|gamedev|backend|deploy> prints a domain how-to"},
			{"framework", "machin framework list|<name>|--vendor", "inspect the embedded framework modules (machweb, db drivers, …); --vendor writes a local copy"},
			{"skill", "machin skill install", "register the agent skills where coding agents look (~/.agents/skills + detected editor dirs)"},
		},
		Domains: guideDomains,
		Proof: guideProof{
			SelfHosting: "The compiler is written in machin: it compiles its own source (lex -> parse -> typecheck -> codegen) and the result reproduces itself byte-for-byte — a full bootstrap fixpoint, zero Go in the toolchain. See CHANGELOG v0.84.0 (compiles itself) and v0.85.0 (the no-Go bootstrap).",
			Benchmarks: []guideBenchmark{
				{
					Axis:      "agent write cost",
					Result:    "a REST+SQLite API: 388 tokens to author (ties Python's 383, ~36% fewer than Go's 527), ships as a 44 KB dependency-free static binary",
					Reproduce: "bench/rest-sqlite: ./run.sh (build+smoke-test); python3 measure.py (token counts, needs tiktoken)",
				},
				{
					Axis:      "native runtime speed",
					Result:    "vs Rust -O3 / Zig ReleaseFast on 4 kernels with byte-identical output: wins fib(40) and a 10^9 integer-sum loop by ~20-25%, ties a float-heavy mandelbrot within 2%, trails an array-heavy sieve by ~1.4x",
					Reproduce: "bench/native-speed: ./run.sh (needs machin, cc, rustc, zig, python3)",
				},
				{
					Axis:      "cold start & ship size",
					Result:    "92.9 kB static binary, 0.49 ms cold start, 108 kB resident memory serving the same HTTP hello endpoint as Node (178 MB / 28.9 ms / 51 MB) — 1916x smaller, 59x faster start, 477x less RAM. Note: that figure is the libc+SQLite case only — see the next row for a TLS-calling app, which is bigger and has its own honest number",
					Reproduce: "bench/cold-start: ./build.sh (sizes); python3 measure.py (cold-start + RSS); ./images.sh (Docker image sizes, needs Docker)",
				},
				{
					Axis:      "TLS-calling app, FROM scratch",
					Result:    "a program using http_get/https_get: 26.5 kB dynamic vs 5.28 MB fully static (OpenSSL + an embedded ~245 KB CA root bundle) — verified in a genuine empty `FROM scratch` container: a real HTTPS request with full certificate verification succeeds, and a known self-signed/untrusted cert is correctly rejected (proving verification is active, not disabled)",
					Reproduce: "bench/tls-static: ./run.sh (build+measure+run; verifies the FROM-scratch case too if Docker is installed)",
				},
				{
					Axis:      "data-race safety",
					Result:    "a textbook shared-counter race (4 threads, no sync): `machin check` catches it at compile time on the untouched code (RACE001) and `--race-safe` refuses the build; Go's compiler accepts it silently (and visibly corrupted the output on this run — confirmed an independent bug, not a false claim, via `go run -race`); Rust's naive translation fails to compile (E0133), and its safe fix needs `Arc<AtomicI64>`/`Ordering` wrapper types machin's zero-annotation channel-based fix doesn't. Numeric output alone is NOT reliable evidence of a race either way (machin's racy build printed the correct sum here too) — which is exactly why compile-time detection beats hoping a test run exposes it",
					Reproduce: "bench/race-freedom: ./run.sh (needs machin, go, rustc)",
				},
			},
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
			{"mmap_file", "(string) -> (int, size)", "memory-map a file read-only -> (pointer-as-int, byte size), or (0,0) on error. MULTI-ASSIGN ONLY: p, n := mmap_file(path). Zero-copy: read the mapped bytes with peek_i8/peek_u8/peek_i32/peek_f32 (pages fault in lazily) instead of read_file_bytes + a copy — for large on-disk buffers like a model checkpoint. Read-only, native only; the mapping lives until the process exits", "io"},
			{"write_file", "(string, string) -> int", "write a file (text; -1 on error)", "io"},
			{"write_file_bytes", "(string, bytes) -> int", "write raw bytes to a file, NUL-safe (-1 on error) — for binary uploads/assets", "io"},
			{"write_file_raw", "(string, int, int) -> int", "write a raw memory region (ptr, nbytes) to a file in one fwrite; for large buffers a bytes value cant hold (e.g. a KV-cache snapshot)", "io"},
			{"read_file_raw", "(string, int, int) -> int", "read a file into a raw memory region (ptr, nbytes) in one fread; returns bytes read, -1 on open error", "io"},
			{"remove", "(string) -> int", "delete a file (0 ok; -1 error)", "io"},
			{"list_dir", "(string) -> []string", "directory entries (excludes . / ..)", "io"},
			{"mkdir", "(string) -> int", "create a directory (0 ok; -1 error)", "io"},
			// cli / process
			{"args", "() -> []string", "command-line args (args()[0] is program path)", "cli"},
			{"env", "(string) -> string", "environment variable (\"\" if unset)", "cli"},
			{"exit", "(int) ->", "terminate the process with a status code", "cli"},
			{"system", "(string) -> int", "run a shell command, return its exit code (-1 if unlaunchable). For process orchestration, e.g. spawning a detached daemon: system(\"./app serve >log 2>&1 &\")", "cli"},
			{"exec", "(string) -> (int, string, string)", "run a shell command and CAPTURE its output -> (exit_code, stdout, stderr). MULTI-ASSIGN ONLY: code, out, err := exec(\"ssh host mongodump ...\"). Captured text is NUL-terminated (redirect binary output to a file in the command). For SSH/mongodump/gzip pipelines and any tool whose output you need", "cli"},
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
			{"parse_float", "(string) -> float", "parse a floating-point number (strtod; 0.0 if non-numeric)", "convert"},
			{"f64_bits", "(float) -> int", "reinterpret a double's IEEE-754 bits as an int64 (for byte-level (de)serialization, e.g. BSON doubles); inverse f64_from_bits", "convert"},
			{"f64_from_bits", "(int) -> float", "reinterpret an int64 bit pattern as a double (inverse of f64_bits)", "convert"},
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
			{"arena_reset", "() -> int", "free the CURRENT goroutine's value-arena chain in place, without ending the goroutine — hands all strings/slice-backings/closure-envs allocated so far back to the OS. The escape hatch for a long-running SINGLE-ACTOR server (one that can't run each request in its own goroutine, e.g. a non-thread-safe store): the main goroutine's arena otherwise grows forever, so call arena_reset() at a quiescent point to keep RSS flat. UNCHECKED, unlike an `arena { }` block (whose escape analysis PROVES nothing escapes): you assert NO arena-allocated value is still reachable — a survivor dangles. Keep any cross-reset state in malloc-backed maps/channels or on disk. Prefer `arena { }` when the lifetime is a lexical scope; reach for arena_reset() only across the request loop of a persistent single-actor server. Returns 0", "memory"},
			{"alloc", "(int) -> int", "allocate n zeroed bytes; returns a pointer (as int). For C buffers/structs to hand to an extern fn", "memory"},
			{"free", "(int) ->", "free a pointer returned by alloc", "memory"},
			{"madvise_free", "(int, int) ->", "drop resident pages of an mmap region (MADV_DONTNEED); re-faults lazily on next access", "memory"},
			{"poke_f32", "(int, int, number) ->", "write a 32-bit float at ptr+byteoffset", "memory"},
			{"poke_i32", "(int, int, int) ->", "write a 32-bit int at ptr+byteoffset (also poke_u8, poke_u16)", "memory"},
			{"poke_ptr", "(int, int, int) ->", "write an 8-byte pointer value at ptr+byteoffset (e.g. a buffer into a struct field)", "memory"},
			{"peek_f32", "(int, int) -> float", "read a 32-bit float at ptr+byteoffset", "memory"},
			{"peek_i32", "(int, int) -> int", "read a 32-bit int at ptr+byteoffset", "memory"},
			{"peek_i8", "(int, int) -> int", "read a signed byte at ptr+byteoffset, sign-extended (also peek_u8, zero-extended) — int8 kernels / binary formats", "memory"},
			{"dot_i8", "(int, int, int) -> int", "signed-byte dot product of two raw buffers (ptr a, ptr b, count) with a 32-bit accumulator — the quantized-matmul group kernel; exact while |sum| < 2^31 (any count <= ~133k of i8*i8), vectorizes where an i64 reduction cannot", "memory"},
			{"dot_q8", "(int, int, int, int, int, int) -> float", "grouped, dual-scaled int8 dot product: (xq, xs, wq, ws, n, gs) -> sum over length-gs groups of (int32 group dot) * xs[g] * ws[g]. xq/wq are int8 buffers (n bytes); xs/ws are fp32 group-scale buffers (n/gs floats). The ENTIRE Q8_0 quantized-matmul inner product in one vectorized call — replaces a per-group dot_i8 + two peek_f32 MFL loop whose call overhead capped throughput (LLM inference). n must be a multiple of gs", "memory"},
			{"matmul_q8_batch", "(int ob, int ostride, int xq, int xs, int wq, int ws, int n, int gs, int B, int lo, int hi) ->", "batched Q8_0 matmul: B activations x one weight-row range, weight read once (prefill GEMM)", "memory"},
			{"matmul_q4_batch", "(int ob, int ostride, int xq, int xs, int wq, int ws, int n, int gs, int B, int lo, int hi) ->", "batched Q4 matmul: like matmul_q8_batch but split-nibble int4 weights (n/2 bytes per row)", "memory"},
			{"matmul_q2_batch", "(int ob, int ostride, int xq, int xs, int wq, int ws, int n, int gs, int B, int lo, int hi) ->", "batched ternary/Q2_0 matmul: like matmul_q8_batch but 2-bit packed weights (n/4 bytes per row, w=(q-1)*scale)", "memory"},
			{"dot_q4", "(int, int, int, int, int, int) -> float", "grouped, dual-scaled int4 dot (LLM matmul kernel): like dot_q8 but split-nibble int4 weights (gs weights in gs/2 bytes; byte k = w[k] low nibble | w[k+gs/2] high nibble, each value+8). Activations int8. Halves weight bytes moved for a memory-bound decode. wq has n/2 packed bytes", "memory"},
			{"dot_q2", "(int, int, int, int, int, int) -> float", "grouped, dual-scaled ternary/Q2_0 dot: 4×2-bit codes per byte (low bits first), w=(q-1)*scale with q in {0,1,2}. Activations int8. wq has n/4 packed bytes", "memory"},
			{"dot_f32", "(int, int, int) -> float", "float32 dot product of two raw f32 buffers (ptr a, ptr b, count), fp32 accumulator — the vectorized inner product for attention scores (q·k) and dense float kernels, where an MFL peek_f32*peek_f32 loop is scalar and call-bound", "memory"},
			{"axpy_f32", "(int, float, int, int) -> void", "AXPY over raw f32 buffers: y[k] += s*x[k] for k<n (ptr y, scale s, ptr x, count n). The attention value accumulation (weighted sum of V rows) and any scaled-add; vectorizes where an MFL peek/poke loop cannot", "memory"},
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
			{"base64_encode_bytes", "(bytes) -> string", "base64-encode raw bytes (binary-safe, unlike base64_encode) — for crypto/wire payloads", "bytes"},
			{"base64_decode_bytes", "(string) -> bytes", "base64-decode to raw bytes (binary-safe; lenient) — e.g. a SCRAM salt or a binary token", "bytes"},
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
			{"bytes_index", "(bytes, bytes, int) -> int", "find a needle in bytes at/after an offset, NUL-safe (-1 if absent); for binary protocols / multipart boundaries", "bytes"},
			{"bytes_concat", "(bytes, bytes) -> bytes", "concatenate two bytes values", "bytes"},
			// crypto over bytes (OpenSSL libcrypto, linked only when used)
			{"rand_bytes", "(int) -> bytes", "n cryptographically-random bytes (CSPRNG)", "crypto"},
			{"sha256_bytes", "(bytes) -> bytes", "SHA-256 of binary -> 32-byte digest (binary-safe, unlike sha256)", "crypto"},
			{"sha1_bytes", "(bytes) -> bytes", "SHA-1 of binary -> 20-byte digest (for legacy auth, e.g. MySQL native password)", "crypto"},
			{"hmac_sha256_bytes", "(bytes, bytes) -> bytes", "HMAC-SHA256(key, msg) -> 32 bytes (binary-safe)", "crypto"},
			{"hkdf_sha256", "(bytes, bytes, bytes, int) -> bytes", "HKDF-SHA256(ikm, salt, info, length) -> length bytes", "crypto"},
			{"pbkdf2_sha256", "(bytes, bytes, int, int) -> bytes", "PBKDF2-HMAC-SHA256(password, salt, iterations, dklen) -> derived key; for password hashing", "crypto"},
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
			{"keccak256", "(bytes) -> bytes", "Keccak-256 (Ethereum's hash — NOT NIST SHA3-256, different padding) -> 32-byte digest; for EIP-712/tx hashing", "crypto"},
			{"secp256k1_pubkey", "(bytes) -> bytes", "secp256k1 public key from a 32-byte private key -> 65-byte uncompressed point (0x04||X||Y); an Ethereum address is the last 20 bytes of keccak256(pub[1:])", "crypto"},
			{"secp256k1_sign_recoverable", "(bytes, bytes) -> bytes", "secp256k1 ECDSA sign (priv32, hash32) -> 65-byte r||s||v (EIP-2 canonical low-S; v is 27/28) — the primitive behind eth_sign/EIP-712/raw tx signing", "crypto"},
			{"secp256k1_recover", "(bytes, bytes) -> bytes", "secp256k1 ECDSA recover (hash32, sig65 r||s||v) -> the 65-byte uncompressed public key (empty bytes if invalid) — same math Solidity's ecrecover uses, for self-checking a signature before broadcasting it", "crypto"},
			{"rsa_generate", "(int) -> (bytes, bytes)", "generate an RSA keypair of `bits` (>=512, default 2048) -> (private PEM, public PEM). MULTI-ASSIGN ONLY: priv, pub := rsa_generate(2048). For SAML SP keys / minting an RS256 signing key. Keygen uses the CSPRNG and is NOT captured by record/replay (generate at setup, not on a replayed path)", "crypto"},
			{"rsa_sign_pkcs1_sha256", "(bytes, bytes) -> bytes", "RSA PKCS#1 v1.5 sign (private PEM, msg) -> signature over SHA-256 (RS256; empty bytes on failure)", "crypto"},
			{"rsa_verify_pkcs1_sha256", "(bytes, bytes, bytes) -> bool", "RSA PKCS#1 v1.5 verify (public PEM SubjectPublicKeyInfo, msg, sig) over SHA-256 (RS256)", "crypto"},
			{"rsa_verify_jwk_sha256", "(bytes, bytes, bytes, bytes) -> bool", "RS256 JWT verify straight from a JWKS: (n, e, msg, sig) where n and e are the base64url-decoded modulus & exponent bytes from the IdP's JWKS. Builds the RSA public key from n/e (no PEM/X.509 needed) and verifies PKCS#1 v1.5 over SHA-256 — the OIDC id_token verification path (issue #484)", "crypto"},
			// sqlite (libsqlite3, linked only when used)
			{"sqlite_open", "(string) -> int", "open/create a SQLite db file -> handle (0 on fail); \":memory:\" for in-memory", "db"},
			{"sqlite_exec", "(int, string[, []string]) -> int", "run SQL with no result; optional []string binds the ? params (injection-safe); 0 ok", "db"},
			{"sqlite_query", "(int, string[, []string]) -> string", "run a SELECT -> a JSON-array-of-rows STRING; optional []string binds the ? params. Decode N rows with parse(rows, []T{}) for a typed slice; json_get for one field", "db"},
			{"sqlite_close", "(int) -> int", "close the database", "db"},
			// regex (POSIX extended)
			{"regex_match", "(string, string) -> bool", "does the ERE pattern match anywhere in s", "regex"},
			{"regex_find", "(string, string) -> string", "first ERE match in s (\"\" if none)", "regex"},
			{"regex_groups", "(string, string) -> []string", "first match's groups: [0]=whole, [1..]=captures ([] if none)", "regex"},
			{"regex_replace", "(string, string, string) -> string", "replace all ERE matches in s with repl", "regex"},
			// json
			{"json", "(any) -> string", "serialize a value to JSON", "json"},
			{"parse", "(string, T{}) -> T", "parse JSON into T (T{} is a type witness); accepts a SLICE witness: parse(jsonArray, []T{}) -> []T (decode whole rows/arrays)", "json"},
			{"json_get", "(string, string) -> (string, string)", "jq-style path -> (value, err); err \"\"/notfound/path/parse. Returns the RAW JSON token: a string value comes back QUOTED (\"Ada\") — strip the quotes, or prefer parse() for whole objects. MULTI-ASSIGN ONLY", "json"},
			{"http_body", "(string) -> string", "body of a raw HTTP message", "json"},
			// net
			{"dial", "(string, int) -> int", "TCP connect host:port -> fd (-1 on fail)", "net"},
			{"listen", "(int) -> int", "open a listening TCP socket on a port", "net"},
			{"accept", "(int) -> int", "accept a connection -> fd", "net"},
			{"peer_addr", "(int) -> string", "remote IP of a connected socket (getpeername), \"\" on error — the real client IP when not behind a proxy", "net"},
			{"socket_timeout", "(int, int) -> int", "cap blocking recv/send on a socket to N ms (0 = none) — anti slow-loris; 0 ok / -1 error", "net"},
			{"read", "(int) -> string", "read a chunk from an fd (blocks); a C string, so it truncates at a NUL — use read_bytes for binary", "net"},
			{"read_bytes", "(int) -> bytes", "read a chunk from an fd as raw bytes, NUL-safe (empty at EOF) — for binary wire protocols (Postgres/MySQL/Redis)", "net"},
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
			{"tls_server_ctx", "(string, string) -> int", "load a cert+key (PEM files) -> a server TLS context handle (0 on fail) — for terminating HTTPS/TLS yourself (no reverse proxy). See serve_tls in framework/machweb.src", "tls"},
			{"tls_accept", "(int, int) -> int", "(ctx, fd) — complete a server-side TLS handshake on an accept()'d fd -> a tls handle (0 on fail)", "tls"},
			{"tls_client_fd", "(int, string) -> int", "(fd, hostname) — the STARTTLS primitive: upgrade an already-connected, plaintext-negotiated fd to a verified TLS handle in place (0 on fail) — e.g. SMTP: dial, EHLO/STARTTLS in plaintext, then upgrade", "tls"},
			{"tls_read", "(int) -> string", "read one chunk from a tls handle (blocks; \"\" at EOF/error) — mirrors read(fd)", "tls"},
			{"tls_read_bytes", "(int) -> bytes", "read one chunk from a tls handle as raw bytes, NUL-safe — mirrors read_bytes(fd)", "tls"},
			{"tls_write", "(int, string) -> int", "write to a tls handle — mirrors write(fd, s)", "tls"},
			{"tls_write_bytes", "(int, bytes) -> int", "write raw bytes to a tls handle, NUL-safe — mirrors write_bytes(fd, b)", "tls"},
			{"tls_close", "(int) -> int", "shut down a tls handle (from tls_accept or tls_client_fd) AND close its underlying fd — the connection is fully torn down, don't also call close(fd) on it", "tls"},
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
			{"eip712-sign", `func eip712_digest(domainSeparator, structHash) (h) { prefix := from_hex("1901")  h = keccak256(bytes_concat(bytes_concat(prefix, domainSeparator), structHash)) }
func main() { priv := rand_bytes(32)  pub := secp256k1_pubkey(priv)
	domainSeparator := keccak256(bytes("name:MyApp,chainId:1"))  structHash := keccak256(bytes("Mail(string contents)hello"))
	digest := eip712_digest(domainSeparator, structHash)
	sig := secp256k1_sign_recoverable(priv, digest)
	println(to_hex(secp256k1_recover(digest, sig)) == to_hex(pub)) }`},
			{"tls-server", `func main() { ctx := tls_server_ctx("server.crt", "server.key")
	if ctx == 0 { println("bad cert/key")  exit(1) }
	srv := listen(8443)
	for { fd := accept(srv)  go handle_one(ctx, fd) } }
func handle_one(ctx, fd) { tls := tls_accept(ctx, fd)
	if tls == 0 { close(fd)  return }
	req := tls_read(tls)
	tls_write(tls, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nhi")
	tls_close(tls) }`},
		},
		Gotchas: []guideNote{
			{"struct-value-semantics", "Structs are VALUE types: passing or assigning one copies it, so a function cannot mutate a caller's struct (and a builder must return the updated struct). For shared mutable state use a map — a reference type, so m[k]=v survives the holder being passed by value (see framework/flags.src)."},
			{"assign-func-scoped", "`:=` is FUNCTION-scoped, not block-scoped (unlike Go): a name declared with `:=` inside an `if`/`else`/`for` block does NOT shadow — every `:=` for that name in the same function binds ONE variable. So the same name given different types in disjoint branches (`steps := \"a,b,c\"` in one branch, `steps := split(s, \",\")` in the sibling) is a hard `type mismatch ...; := does not shadow — variables are function-scoped` error, even though Go would compile it as two independent block-locals. Fix: use distinct names (`stepsCsv` / `stepsList`), or declare once before the branches and only assign inside. See also package-globals (`:=` shadows a package global but not another local)."},
			{"map-comma-ok", "There is no map comma-ok: `v, ok := m[k]` does NOT compile. A read of an absent key returns the value type's zero value; use has(m, k) to test presence. (comma-ok is for channel receives: `v, ok := <-ch`.)"},
			{"parse-witness", "parse(s, T{}) needs a value of T as a type witness, e.g. `u := parse(body, User{})`. A SLICE witness decodes an array: `parse(jsonArray, []T{}) -> []T`. For schemaless extraction use json_get(s, path); json(x) serializes any value."},
			{"sqlite-rows-decode", "sqlite_query returns a JSON-array-of-rows STRING — decode the whole result with parse(rows, []T{}) to get a typed []T you can range over (this is the row-iteration idiom; columns map to struct fields by name). Reach for json_get only to pull ONE field, and remember json_get returns the raw JSON token: a string field comes back QUOTED. Always use parameterized sqlite_exec/query (?, []string{...}) — never string-concat user input into SQL."},
			{"mongo-client", "framework/mongo.src + framework/bson.src are a pure-MFL MongoDB client (the OP_MSG wire protocol + a BSON codec) — no driver/cgo. bson.src: build a document with bson_new() then bson_str/bson_i32/bson_i64/bson_bool/bson_null/bson_subdoc/bson_subarr, finalize with bson_finish; decode with bson_to_json(bytes) -> JSON string. mongo.src: mongo_connect(host, port); mongo_insert_one(db, coll, bson_finish(doc)); mongo_find_all(db, coll) -> a JSON-array STRING (so parse(docs, []T{}) decodes; _id ObjectId comes back as a hex string); mongo_count; mongo_drop; mongo_close. POOL (concurrent server): mongo_pool_init(n, host, port, user, pass) (user \"\" = no auth), then per request c := mongo_acquire(); mins/mfind/mfindall/mfindid/mdel/mdelid/mcount/mdrop/mcmd(c, ...) / mauth(c, ...); mongo_release(c). The global mongo_* calls are one shared connection. FILTERS + _id: mongo_find(db, coll, filter) (filter is a finished BSON doc), mongo_find_by_id(db, coll, idhex)/mongo_delete_by_id(db, coll, idhex)/mongo_delete(db, coll, filter); build an ObjectId from its hex with bson_oid(acc, key, idhex) (the decoder renders _id as that hex). AUTH: mongo_auth(authdb, user, password) does SCRAM-SHA-256 right after connect (authdb usually \"admin\"). Types: string/int32/int64/double (via f64_bits)/bool/null/ObjectId/binary; mongo_find_all follows the cursor (getMore) so it returns ALL docs, not just the first batch. See docs/NORTH-STAR-BACKEND.md."},
			{"mysql-client", "framework/mysql.src is a pure-MFL MySQL/MariaDB client (wire protocol, no libmysql). mysql_connect(host, port, user, password, db) -> ok (mysql_native_password / SHA-1 auth; for MySQL 8 create the user IDENTIFIED WITH mysql_native_password); mysql_query(sql) -> a JSON-array-of-rows STRING (numeric columns unquoted, so parse(rows, []T{}) decodes); mysql_exec(sql) -> affected rows; mysql_escape(s) for single-quoted values; mysql_close(). POOL: mysql_pool_init(n, host, port, user, pass, db), then mysql_acquire() + myq(c,sql)/myx(c,sql) + mysql_release(c). Text protocol; caching_sha2 + prepared statements are follow-ups. Drove sha1_bytes into machin."},
			{"redis-client", "framework/redis.src is a pure-MFL Redis client (RESP protocol over dial(), no client lib). redis_connect(host, port) [+ redis_auth(password)]; typed helpers redis_set/redis_setex(key,secs,val)/redis_get(key)->(val,ok=0 if missing)/redis_del/redis_exists/redis_incr/redis_expire/redis_rpush/redis_lpush/redis_lrange/redis_keys; redis_close(). General escape hatch redis_cmd(args []string) -> (val, ok) (ok=0 on a -error or nil reply); array replies (LRANGE/KEYS) come back as a JSON-array STRING -> parse(val, []string{}). For cache, sessions, counters, rate-limits, queues. v1 = string values (an embedded NUL would truncate). CONCURRENT SERVER -> POOL: redis_pool_init(n, host, port) once, then per request c := redis_acquire(); rset/rsetex/rget/rdel/rincr/rcmd(c, ...); redis_release(c) (the connection-taking r* helpers; the global redis_* are one shared connection)."},
			{"sso-oauth", "framework/sso.src is OAuth2/OIDC single sign-on (\"log in with Google/Microsoft/…\") in pure MFL on top of machweb sessions. Fill an OAuthProvider{auth_url, token_url, userinfo_url, client_id, client_secret, redirect_uri, scope}; route GET /login -> sso_begin(p, secret) (302 to the provider + a signed oauth_state CSRF cookie) and GET /callback -> sso_complete(p, secret, req) -> (profile_json, ok) (verifies state, exchanges the code via http_request, fetches userinfo). On ok, json_get(profile, \".sub\"/\".email\") and set_session your own login. Identity is from the userinfo endpoint, so no JWT/RSA needed. Keep client_secret + session secret server-side (env)."},
			{"machweb-sessions", "framework/machweb.src does cookies + signed sessions, plus redirect(url) (302 + Location) and query(req, name) (a ?query-string param, url-decoded). Request: cookie(req, name) -> value. Response (a value type — chain the helpers, they return a new Response): set_cookie(res, name, value) / clear_cookie(res, name) add a Set-Cookie with safe defaults (Path=/; HttpOnly; SameSite=Lax). Login sessions: set_session(res, secret, name, value) stores value + an HMAC-SHA256 tag the client can't forge; get_session(req, secret, name) -> (value, ok) returns ok==1 only if it verifies. Keep secret server-side (env). The value is signed, not encrypted — store an id, not secrets."},
			{"postgres-client", "framework/postgres.src is a pure-MFL PostgreSQL client (wire protocol v3 over dial(), SCRAM-SHA-256 auth) — no libpq/cgo. Single connection: pg_connect(host, port, user, db, password); pg_query(sql) (simple/trusted) or pg_exec(sql, []string params) ($1,$2 bound server-side via the extended protocol — injection-safe; SELECT and INSERT/UPDATE/DELETE); pg_disconnect(). Both return a JSON-array-of-rows STRING like sqlite_query, so parse(rows, []T{}) decodes (numeric/bool unquoted via result OIDs). CONCURRENT SERVER -> use the POOL (machweb runs each request in its own goroutine; a shared connection would interleave): pg_pool_init(n, host, port, user, db, password) once, then per request c := pg_acquire(); pgq(c, sql) / pgx(c, sql, params); pg_release(c). The pool is an async channel of authenticated fds; each acquired PgConn has its own buffer. See docs/NORTH-STAR-BACKEND.md."},
			{"multi-assign-only", "http_get and json_get return multiple values; use `a, b, c := http_get(u)`. Calling them as a single value is a compile error."},
			{"int-float-no-implicit", "There is NO implicit int->float. Only a flexible numeric LITERAL (e.g. 5) promotes against a float; a CONCRETE int — a fn return, byte_at, len, a typed param, or an int-slice element — is a hard `int vs float` mismatch with a float. Wrap it: float(byte_at(b,0)) / 2.0. (int() goes the other way.) This also applies to f32/f64 struct fields in FFI cstructs."},
			{"ffi-opaque-handle", "For a by-value C struct that contains pointers (raylib Sound/Music/Font/Texture with internals, FILE wrappers, ...), declare an OPAQUE cstruct with an empty body: `cstruct Sound {}`. machin holds the real C struct by value and passes it back to fns without naming its fields — receive it from a fn, store it (incl. []Sound), pass it on; no construct or .field. (A single pointer is simpler: the `ptr` FFI type, held as an int.)"},
			{"ffi-nested-cstruct", "A cstruct field may be ANOTHER cstruct (by-value struct of structs) — declare the inner one first. e.g. `cstruct Vector3 { x f32 y f32 z f32 }` then `cstruct Camera3D { position Vector3 target Vector3 up Vector3 fovy f32 projection i32 }`; construct with nested literals `Camera3D{Vector3{0,10,10}, Vector3{0,0,0}, Vector3{0,1,0}, 45.0, 0}`. Required for 3D (BeginMode3D) and 2D cameras. (Camera/orbit trig: machin has native sin/cos/sqrt/pi etc. — see the `math` builtins.)"},
			{"ffi-raw-buffers", "Hand a C API raw buffers + structs via raw memory (pointers are ints): `p := alloc(nbytes)` (zeroed), `poke_f32/poke_i32/poke_u8/poke_ptr(p, byteOffset, v)`, `peek_f32/peek_i32`, `free(p)`. Struct pointer params: a cstruct FIELD may be `ptr` (a pointer, held as int) so you declare a struct like raylib Mesh `{vertexCount i32 vertices ptr colors ptr vaoId u32 vboId ptr}` and the C compiler lays it out (no offsets). Pass a cstruct by pointer with writeback via an INOUT param `Name*` (`fn UploadMesh(Mesh*, bool)` — arg must be a variable). Pass a raw pointer by value with `ptr` (becomes void*->any T*); deref a raw pointer to a by-value struct with `*Name`. GPU mesh: build vertex/color arrays with alloc/poke, `mesh := Mesh{vcount, vcount/3, vbuf, cbuf, 0, 0}`, `UploadMesh(mesh, false)`, `LoadModelFromMesh(mesh)`. (see machin-game-demo-planet)"},
			{"eval-order", "Operands and arguments evaluate left-to-right (as in Go), including side effects: `f() + g()` runs f() before g(). Holds for binary ops, call args, slice/struct literals, and multi-return lists."},
			{"composite-literal", "T{...} literals need T to be a known struct type at parse time. `machin encode` registers all `type` decls first, so this just works in normal builds."},
			{"stdout-buffering", "libc fully buffers stdout when it's a pipe; a streaming program must call flush() after a write to appear promptly downstream. A TTY is line-buffered."},
			{"channels-cross-goroutine", "Values sent over a channel are deep-copied across the goroutine/arena boundary (strings fast; slices/maps/structs via JSON), so they survive the sender goroutine. Channels of closures/funcs are not deep-copied."},
			{"data-race-safety", "`machin check` INFERS data-race freedom with no annotations (the guarantee Rust needs Send/Sync for) and reports races as errors (phase `race`: RACE001 write/write, RACE002 read/write, RACE004 use-after-move); `build|run --race-safe` refuses to compile one. What races: a slice/map (or struct-with-slice field) shared across goroutines and written by one; a package global touched concurrently (even a scalar — globals are one shared cell); a captured slice in a closure passed to a `go`-spawned function. The safe pattern is SHARE BY COMMUNICATING: give each goroutine its own data and pass results over a channel — after `ch <- v`, don't touch `v` (ownership moved). Reads-only sharing is fine; a value written before a `go` (or read after joining the goroutine via a channel receive) is ordered, not a race."},
			{"select-closed", "A closed channel makes its select receive case ready, firing repeatedly (with ok==false if you wrote `case v, ok := <-ch:`). Detect close and stop selecting on it."},
			{"no-tls-without-https", "Plain dial/listen/accept are TCP only — no TLS. For a TLS client REST/WS call, use https_get/https_post or wss_*. For anything else over TLS (a raw dial()'d or accept()'d fd), see tls_client_fd/tls_server_ctx/tls_accept (server-tls-v1 gotcha) — added in v0.92.0, so this is no longer 'there is no raw TLS socket'."},
			{"server-tls-v1", "tls_server_ctx/tls_accept let machweb terminate HTTPS itself (serve_tls in framework/machweb.src) — no reverse proxy needed for a simple/internal service. v1 scope: one cert per tls_server_ctx (no SNI multi-cert virtual hosting), no client-cert verification (not mutual TLS), no ACME/auto-renewal (bring your own cert+key, renew it yourself), and serve_tls does NOT support res.is_hijack/res.is_stream (protocol upgrades, SSE) yet — those get a 501 rather than misbehaving; use serve behind a reverse proxy for those endpoints. tls_client_fd is the STARTTLS primitive (upgrade an already-connected, plaintext-negotiated fd to TLS in place, e.g. after SMTP EHLO/STARTTLS) — it verifies the remote cert exactly like https_get does, so an untrusted/self-signed cert is rejected, not silently accepted."},
			{"falsification", "`machin check` (and `machin falsify`) run a bounded falsifier that hands you the EXACT input breaking a function — FALS001 index-out-of-range, FALS002 divide/modulo-by-zero, FALS010 postcondition-violated — as an advisory `warnings` entry (phase `falsify`, never fails the build). Contracts: put `requires <bool-expr>` / `ensures <bool-expr>` clauses on a signature (after the returns, before the body) to state pre/postconditions the falsifier checks — `requires` filters inputs (suppresses false bugs from invalid inputs), `ensures` (over named returns) is checked after the body and a violation is FALS010. Unlike the race pass it is unsound-complete: it finds bugs but NEVER proves absence, so a clean result means 'no bug within the bounds' (int/float/string domains, slice length ≤ 3), not 'correct' — don't read silence as a proof. Every reported finding IS a real bug (false-positive-free: only fully-modeled concrete paths are reported; anything touching an unknown call/FFI/unsupported construct is silently skipped as inconclusive, so complex functions may report nothing). `machin falsify --repro <dir>` writes a runnable repro per finding that panics under `--safe` — commit it as a regression test."},
			{"eip712-uint256", "keccak256/secp256k1_pubkey/secp256k1_sign_recoverable/secp256k1_recover give the primitives for Ethereum-style signing (EIP-712 typed data, eth_sign, raw tx signing), but a full EIP-712 ABI encoder is NOT a builtin — you assemble domainSeparator/structHash by hand from keccak256 + bytes_concat (see the eip712-sign idiom). The real gap: Solidity `uint256` struct fields (token IDs, amounts, salts) routinely exceed MFL's 64-bit `int`, so encode them as 32-byte big-endian `bytes` (from a hex string the caller already has, e.g. from an API) rather than as `int` — MFL has no builtin decimal-string-to-bytes32 bignum conversion. An Ethereum address is the last 20 bytes of keccak256(pub[1:]) (pub is the 65-byte uncompressed key, so skip its 0x04 prefix first — bytes_sub(pub, 1, 65))."},
			{"floats-over-chan-json", "A slice/map channel element round-trips through JSON, which formats floats with %g (not bit-exact for pathological doubles)."},
			{"memory", "Per-goroutine arena, reclaimed in bulk when the goroutine returns; wrap a hot allocating loop in `arena { ... }` to keep peak memory flat. Build with --safe for bounds/overflow/div-zero checks."},
			{"wasm-target", "`machin build app.mfl --target wasm` compiles to a WebAssembly reactor module (needs `zig` as the C->wasm compiler; override with ZIG=). Mark host-callable functions `export func name(...)` — they become wasm exports under their clean name (and are reachability roots, so a wasm module needs no main). A headerless `extern \"env\" { fn dom_set(string) }` becomes a wasm IMPORT the JS host supplies (the `extern \"<lib>\"` name is the import module). Marshaling host-side: machin ints are i64 -> pass/return BigInt; strings are a pointer into the exported `memory` (decode NUL-terminated UTF-8). App state can live in machin via package globals (`var count = 0`), which persist across export calls. See docs/NORTH-STAR-WEB.md."},
			{"package-globals", "A top-level `var name = expr` is a package GLOBAL: mutable, shared by every function, type inferred from the initializer + uses. It PERSISTS across calls (unlike a local), so a wasm export can hold state between host calls (`var count = 0` + `export func bump(d){count=count+d}`). `=` assigns the global; `:=` makes a local (and may shadow it). Globals work everywhere incl. closures (a captured global is referenced directly), and for any type incl. make-maps and slices. Init runs before main / at wasm `_initialize`."},
			{"regex-silent-fail", "regex_match/regex_find/regex_groups/regex_replace take a POSIX ERE pattern; an invalid pattern (regcomp failure, e.g. an unmatched paren) is NOT an error — there's no error channel, so each builtin silently returns its benign default instead (regex_match->false, regex_find->\"\", regex_groups->[], regex_replace->s unchanged). A typo in a pattern therefore looks like \"no match\" rather than failing loudly. Also, regex_replace's repl is inserted LITERALLY — there is no \\1/$1 backreference or group substitution (unlike sed or Go's ReplaceAll)."},
			{"lambda-and-builtin-names", "Two rules when defining functions/closures: (1) a lambda (`func(){...}`) has NO named returns — `func() (s) { s = x }` does NOT parse; use `func() { return x }`. (2) A user function may NOT be named like a builtin (`flush`, `len`, `str`, `keys`, `contains`, ...) — it is a compile error (the builtin would win at call sites, silently ignoring your function). Pick another name. (An `extern` MAY shadow a builtin — that's intentional for FFI.)"},
			{"reactive-runtime", "`framework/reactive.src` is a fine-grained reactive runtime (signals + a patch list) for wasm UIs. `signal(v)`/`get`/`set`; `computed(func(){ return ... })` is a memoized derived signal; `bind(slot, func(){ return str(...) })` patches a DOM text slot on change; `each(container, func(){ get(ver)  return csv(ids) }, func(k){ return html })` does keyed list reconciliation (keys as a CSV string; emits insert/remove/order). Only reactions that read a changed signal recompute, and only changed text/keys patch. TEMPLATING (v0.55.0): declare markup+bindings together — `slot(name, compute)` and `list(name, keys, item)` return markup AND queue a reaction; `mount(root, html)` sets the root HTML once then flushes them (so `mount(\"app\", \"<h1>x</h1>\" + slot(\"n\", fn) + list(\"items\", keys, item))` is the whole component). ISOMORPHIC (v0.56.0): `slot` embeds its initial value, and `hydrate(html)` attaches bindings to an SSR server's existing DOM (by data-s names) instead of re-rendering — so one component SSRs on the server and hydrates in the browser (see boilerplate-cli-ui-machin-isomorphic). Host supplies dom_mount/dom_patch/list_insert/list_remove/list_order. Drove `[]func` (v0.53.0)."},
		},
	}
}

// cmdGuide prints the feature catalog: JSON by default (machine-readable, the
// intended agent entry point), or a dense prose form with --text.
func cmdGuide(args []string) error {
	text := false
	for i, a := range args {
		switch a {
		case "--text", "-t":
			text = true
		case "--json":
			text = false
		case "--skill":
			name := ""
			if i+1 < len(args) {
				name = args[i+1]
			}
			switch name {
			case "start", "machin":
				fmt.Print(skillStart)
			case "web":
				fmt.Print(skillWeb)
			case "gamedev", "game":
				fmt.Print(skillGamedev)
			case "backend":
				fmt.Print(skillBackend)
			case "deploy":
				fmt.Print(skillDeploy)
			default:
				return fmt.Errorf("unknown skill %q — available: start, web, gamedev, backend, deploy (see `machin guide` domains)", name)
			}
			return nil
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

	b.WriteString("COMMANDS (the CLI surface)\n")
	for _, c := range g.Commands {
		fmt.Fprintf(&b, "  %s\n      %s\n", c.Usage, c.Summary)
	}
	b.WriteString("\n")

	b.WriteString("DOMAINS — building something specific? read the how-to FIRST (don't reverse-engineer demos):\n")
	for _, d := range g.Domains {
		fmt.Fprintf(&b, "  %-8s %s\n           %s\n", d.Name, d.Howto, d.Summary)
	}

	b.WriteString("\nPROOF — measured, not claimed; reproduce any of it yourself:\n")
	fmt.Fprintf(&b, "  self-hosting: %s\n", g.Proof.SelfHosting)
	for _, bm := range g.Proof.Benchmarks {
		fmt.Fprintf(&b, "  [%s] %s\n      reproduce: %s\n", bm.Axis, bm.Result, bm.Reproduce)
	}

	b.WriteString("\nTYPES\n")
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
