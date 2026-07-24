# Changelog

## Unreleased

## v0.116.0

- **`x509_pubkey(cert_der) -> (n, e)` — the X.509 arm of pure-MFL SAML SSO**
  (issue #484). Extracts an RSA public key (raw big-endian modulus `n` and
  exponent `e`) from a DER-encoded X.509 certificate — the base64-decoded bytes of
  a SAML metadata `<ds:X509Certificate>`. The output plugs straight into
  `rsa_verify_jwk_sha256(n, e, msg, sig)`, so an RSA-SHA256 signature can be
  verified against the IdP's embedded cert with no JWKS. Non-RSA (EC/Ed25519)
  certs return two empty buffers so a caller can detect "not an RSA cert". A new
  pure-MFL helper `sso_x509_rsa_verify(cert_b64, signed, sig_b64)` in
  `framework/sso.src` wraps the extract-and-verify flow (XML whitespace tolerated).
  Verified end-to-end against genuine `crypto/x509` certs + `crypto/rsa`
  signatures. XML Exclusive Canonicalization (c14n) + `<ds:Signature>` parsing —
  the rest of SAML — remain open under #484.

## v0.115.0

- **Windows cross-compile target (`--target windows`), Phase 0** — `machin build
  app.mfl --target windows` produces a Windows x86-64 `.exe` via `zig cc -target
  x86_64-windows-gnu` (needs `zig`, no other toolchain; override with `ZIG=`).
  This first phase gets the POSIX-independent core compiling under mingw-w64:
  stdio, strings/maps/slices, structs, `json()`, arena blocks + `arena_reset()`,
  **goroutines + channels** (mingw's winpthreads, so the concurrency runtime is
  unchanged), math, and file/dir I/O. Portability is by `#ifdef _WIN32` guards in
  the emitted runtime (non-portable headers, `WEXITSTATUS`, `SIGPIPE`, `mmap`,
  `localtime_r`/`gmtime_r`, `mkdir` arity), so the same C still builds native and
  wasm unchanged. Programs that use networking (winsock2), TLS/crypto
  (OpenSSL-for-Windows), terminal raw mode, SQLite, or POSIX regex are rejected at
  build time with an actionable message — those are later phases. Part of #517.

## v0.114.0

- **`json()` is no longer O(n²)** — the generated serializers built their result
  with chained `mfl_cat`, each of which recopied the entire accumulated prefix,
  and every intermediate prefix stayed in the (never-collected) arena as
  permanent garbage. Serializing a 4,000-element `[]struct` (~2 MB of output)
  took ~5 s and **8.2 GB** peak RSS; 40k was effectively a hang. The serializers
  now append into a growable string builder (`mfl_sb`, doubling, malloc-backed,
  copied once into the arena at the end), so serialization is O(total output) in
  both time and memory: the same 4k case is now ~0.01 s / ~12 MB, and 40k is
  ~0.07 s / ~110 MB. Output bytes are unchanged. Fixes #520.

## v0.113.0

- **`arena_reset()` builtin** — frees the current goroutine's value-arena chain
  in place, without ending the goroutine, handing all strings / slice backings /
  closure environments allocated so far back to the OS. The escape hatch for a
  long-running **single-actor** server (one that can't run each request in its
  own goroutine, e.g. a non-thread-safe store like grange): the main goroutine's
  arena otherwise grows monotonically. Call it at a quiescent point to keep RSS
  flat — a 400k-iteration string churn drops from ~166 MB peak to ~2 MB with an
  identical result. UNCHECKED, unlike an `arena { }` block (whose escape analysis
  *proves* nothing escapes): the caller asserts no arena-allocated value is still
  reachable; keep cross-reset state in malloc-backed maps/channels or on disk.
  Fixes #523.

## v0.112.0

- **Ternary / Q2_0 quantized matmul kernels** (`dot_q2`, `matmul_q2_batch`) for
  Prism/anvil Ternary-Bonsai inference: 2-bit packed weights (4×2-bit codes per
  byte, low bits first) with `w=(q-1)*scale` dequant (`q ∈ {0,1,2}`), activations
  int8, group scales fp32. Also adds the missing `matmul_q4_batch` (split-nibble
  int4 weights) referenced by existing Q4 engines — same batched shape as
  `matmul_q8_batch` (B activations × one weight-row range, weight read once).

## v0.111.0

- **Pure-MFL RS256 id_token / JWT verification** (`sso_verify_rs256` in
  `framework/sso.src`, part of issue #484). Verifies an RS256-signed JWT against a
  provider's JWKS in pure MFL on top of the `rsa_verify_jwk_sha256` builtin — no
  PEM/X.509, just the JWKS `n`/`e`. Pins the header `alg` to exactly `RS256`
  **before** touching any key material, so a forged `none`/`HS256` token is
  rejected outright (the classic alg-confusion attack); selects the signing key by
  `kid`; and returns the decoded claims only when the signature verifies. The
  caller still validates `exp`/`iss`/`aud`. Tested end-to-end against genuine
  `crypto/rsa`-minted tokens (valid, tampered, alg-confusion, wrong-kid).

## v0.110.0

- **codegen: NULL-string segfault fix.** `parse()` zero-initializes structs with
  `{0}`, so a missing string field is `NULL` (`char*`), not `""`. `len()` / `charat`
  called `strlen(s)` directly, and `strlen(NULL)` is undefined behavior (segfault).
  `len()` on a string now routes through `mfl_s()` (which normalizes `NULL`→`""`),
  and `mfl_charat` uses the NULL-guarded `mfl_strlen_cached`, so `len()` /
  indexing a missing parsed field is `0` / `""` instead of a crash. (Surfaced
  building grange, where a missing JSON field crashed on `len()`.)

## v0.109.0 — Evidence: certify, deadlock-freedom, superoptimization, arena safety

The **evidence** release — the binary now comes with machine-checkable proof across
correctness, concurrency, and memory, plus the crypto/replay/diagnostics work.

- **Self-certifying compiler — `machin certify` (translation validation).** Per
  build, prove the emitted binary matches the source semantics within bounds, or
  hand back the exact miscompiling input. Slice 1.1 + 1.2 (validates ints, strings,
  structs, and `[]int` via `json()`), with a continuous codegen-path gate.
- **Deadlock-freedom — the concurrency capstone.** A runtime detector reports and
  exits instead of hanging (`fatal: deadlock — all N goroutine(s) blocked…`) with a
  causal wait-cycle report (which goroutine waits on which channel); a compile-time
  finder **DL001** (a receive on a channel nothing ever sends to or closes — a
  guaranteed deadlock, found statically, false-positive-free); self-hosted
  oracle-identical to the Go pass; plus select-spin detection and opt-in
  `--deadlock-strict` for I/O-wait hangs.
- **Provable superoptimization — propose → prove → quantify → discover.**
  `machin equiv f g <file>` (bounded equivalence oracle), `machin optimize <file>`
  (oracle-gated peephole rewrites — unsound ones refuted with a counterexample),
  a static latency cost model quantifying each rewrite, and `machin superopt <fn>`
  (enumerative synthesis that discovers cheaper equivalents the fixed rules can't,
  with overfit guards), now with a richer shift/constant grammar + cost-ordered search.
- **Arena escape analysis — memory safety without a borrow checker.** `machin check`
  proves nothing allocated in an `arena { }` block outlives it (bulk reclamation is
  sound), reporting `ARENA001` (a value escaping the block) and `ARENA002` (a
  `return` inside the block). Interprocedural provenance, closure-capture coverage,
  per-place (struct-field/element) granularity, and a self-hosted oracle-identical port.
- **RSA crypto** — PKCS#1 SHA-256 sign/verify + JWK RS256 verify (toward pure-MFL
  RS256 JWT / SAML).
- **Record/replay Phase 3e** — `machin replay --print <var>`, a value-query replay
  debugger over a captured run.
- **Sharper typecheck diagnostics** — a type mismatch now names the offending
  identifier + its enclosing function and line, and explains that `:=` is
  function-scoped (does not shadow) when disjoint branches collide.
- **codegen fixes** — escape `?` in emitted C string literals to defeat trigraph
  corruption (`??'`→`^`); emit 64-bit (`LL`) integer literals so large products no
  longer overflow a 32-bit `int`; copy map `keys()` into arena storage to fix a
  use-after-free on a retained result after `delete`.

## v0.108.0

- **The Falsifier — a compile-time bounded bug-finder and bounded prover, new
  `machin falsify` command and a `falsify` phase in `machin check`.** It
  enumerates small concrete inputs to each function and reports the *exact* input
  that trips a runtime-checked property: `FALS001` index-out-of-range, `FALS002`
  divide/modulo-by-zero. Findings are **advisory** (a separate `warnings` channel;
  they never fail `check`/`build`) and **false-positive-free by construction** —
  a counterexample is emitted only from a fully-modeled concrete path; anything it
  can't model (opaque call, unsupported construct) makes that input *inconclusive*
  and is dropped. The analysis reaches through calls (interprocedural inlining) and
  struct fields. `machin falsify --repro <dir>` writes one runnable `.mfl` per
  finding that panics under `--safe` at exactly the predicted trap — an
  auto-promotable regression test. `--json` returns a verdict envelope
  (`counterexamples`, per-function `verdict`, and the search `bounds`) that never
  overstates. Pointed at the example corpus it surfaced two real latent bugs
  (`divmod` ÷0, `minmax` indexing an empty slice).
- **Design-by-contract: declarative `requires` / `ensures` clauses on a function
  signature.** `requires <expr>` (over params, after the return list, before the
  body) is a precondition that *filters* the input domain — an input failing it is
  the caller's fault, so it suppresses a would-be `FALS001`/`FALS002`. `ensures
  <expr>` (over params + named returns) is a postcondition; an input satisfying
  every `requires` that makes an `ensures` false is a `FALS010` counterexample.
  Predicates are tri-state, so a non-bool / trapping / unmodeled predicate is
  inconclusive, never a false positive. Codegen ignores contracts (analysis-only).
- **Honest bounded proof: `machin falsify --prove`.** Switches from the sparse
  bug-finding sample to a *dense, fully-covered* bounded space (int `[-8,8]`, slices
  up to length 4, all bool/struct combinations) and enumerates every input — a
  bounded model check by exhaustion, no SMT. Exhausting it clean earns `proved`
  (finite/bool domains — a total proof) or `proved-bounded` (int/`[]int` — only up
  to the reported `bounds`, never "correct"); float/string params and any
  unmodeled path block a proof (verdict stays `clean`/`unknown`), so a bound is
  never overstated. Dense enumeration also finds bugs the sparse sample misses.
- **The Falsifier is self-hosted.** The whole pass — counterexample search,
  contracts, and bounded proofs — is ported to MFL (`selfhost/falsify.src`) and
  verified byte-for-byte against the Go reference via `machin falsifytest`, so the
  machin-in-machin compiler both compiles *and* falsifies *and* proves, provably
  identically.
- **Fixed a segfault when `parse()` filled a struct whose string fields were
  absent from the JSON.** The generated JSON parser initialized the result with
  `{0}`, so any string field not present in the payload stayed a NULL `char*`
  instead of `""`. Touching such a field — `len()`, concatenation, printing —
  dereferenced NULL and crashed (e.g. `strlen(NULL)`), which surfaced as an
  exit-255/segfault when the unset field flowed through a helper back into a
  range loop. The parser now seeds omitted string fields (recursively, through
  nested structs) to `""` via the same `stringZeroInits` path the struct-literal
  codegen already uses. See #449.

- **Fixed `&&` / `||` not short-circuiting: the right operand was evaluated
  even when the left already decided the result.** When either operand had a
  side effect (a call or channel receive), codegen hoisted *both* operands
  into temporaries via `seqExprs` before combining them, so the right operand
  always ran. This broke guard idioms like `if p != nil && p.ok()` and, under
  `--safe`, could turn a guarded expression into a crash (e.g.
  `if j > 0 && boom(xs) == "a"` still called `boom` when `j == 0`). Logical
  `&&`/`||` are now emitted directly as C's short-circuiting operators, which
  guarantee left-to-right evaluation with a sequence point between operands.
  See #437.

## v0.107.0

- **Fixed a use-after-free/segfault: a `func` literal passed directly as a
  `go` call argument (e.g. `go f(func(){ return v })`) could dangle once the
  spawning goroutine's arena was reclaimed.** The closure's captured-env
  struct, and any boxed param/local shared with a nested closure, were
  allocated with the arena-scoped `mfl_alloc`/`mfl_calloc` instead of plain
  `malloc`/`calloc` — so the closure's captured state could be corrupted or
  crash the process once read from the spawned goroutine, after the spawner
  returned and freed its arena. Scalar (int/bool/float) captures are now
  arena-independent, matching how channels and maps already manage their own
  memory; string/slice/map captures reassigned through the box remain a
  separate, still-open gap. See #314.
- **Completed the #314 fix: the DATA a captured variable holds now also
  survives the arena boundary, not just the pointer chain.** The previous
  entry made the env struct and capture boxes malloc-stable, but a captured
  string's bytes, a captured struct's string fields, and a captured
  slice/map's backing still lived in the spawning goroutine's arena — so
  `go f(func(){ ...label... })` from a short-lived goroutine still read
  corrupted memory once the spawner's arena was reclaimed (the original
  repro was never actually fixed; verified before and after). `goStmt` now
  classifies each capture of a closure literal passed to `go` and
  freeze/thaws its box contents across the boundary — the same machinery
  channel sends and plain `go` arguments already use (#310): string-offset
  copy for strings and structs of strings, a JSON round-trip for anything
  holding a slice or map; scalars need nothing. As with a channel send,
  ownership moves: the spawner must not keep mutating a captured variable
  after the `go` statement. A closure passed as a plain *variable* (not a
  literal at the call site) keeps the documented shared-value caveat. See
  #314.

## v0.106.0

- **Fixed a high-severity data-corruption bug: `parse()` silently mangled
  `\uXXXX` JSON escapes instead of decoding them.** `{"s":"em—dash"}`
  parsed to the literal text `emu2014dash` instead of `em—dash` — the
  backslash was dropped and the four hex digits leaked through as ordinary
  characters. Common in practice: many JSON producers (including LLM APIs)
  escape non-ASCII characters this way. Now correctly UTF-8 encodes the code
  point, including surrogate-pair decoding for astral characters (emoji,
  etc); a malformed `\u` escape degrades safely to literal text rather than
  crashing. See issue #311.
- Documented that MFL string literals have no `\uXXXX` escape form (SPEC.md)
  — embed non-ASCII as raw UTF-8 in source files instead.

## v0.105.0

- **Self-hosted compiler: ported the #310 `go`-argument use-after-free fix to
  `selfhost/cgen.src`.** The self-hosted compiler's `go`-statement codegen now
  emits the same `mfl_freeze_strs`/`mfl_thaw_strs` arena-boundary protection
  as the reference compiler, reusing the existing channel-send machinery
  (`chan_str_offsets`/`chan_needs_json`). A `go` call argument needing the
  JSON round-trip (a slice/map) is marked unsupported, consistent with
  JSON-mode channels (#293) — the self-hosted compiler doesn't have JSON
  codegen yet. Verified byte-identical against the reference compiler's C
  output on a targeted struct-of-strings case, corpus parity restored
  (`verify-cgen.sh`: 411/0, was 408/3 after #310 shipped), and the
  self-hosting fixpoint still holds (`verify-fixpoint.sh`: PASS). See #313.

## v0.104.0

- **Fixed a high-severity use-after-free: `go f(args)` no longer dangles when
  the spawning goroutine's arena is freed before the new goroutine reads its
  arguments.** A detached goroutine gets its own fresh arena; any argument
  holding a pointer into the SPAWNING goroutine's arena (a string, or a
  struct/slice/map containing one) could dangle once that arena was reclaimed
  — the exact pattern of a machweb request handler doing
  `go background_work(ag, conv)` then returning, observed corrupting strings
  in a production log. `go` call arguments are now deep-copied across the
  arena boundary exactly like channel sends already were (string-offset
  freeze/thaw for plain strings and structs of strings, a JSON round-trip for
  anything containing a slice or map); scalars are unaffected, since they were
  never heap-allocated. See issue #310.

## v0.103.0

- **Docs: added a "C FFI (extern)" section to `docs/LANGUAGE.md`.** The language
  reference had no coverage of `extern` blocks at all — basic syntax (`header`/
  `link`/`cflags`/`fn`), the FFI scalar type table (`i8`..`u64`/`f32`/`f64`/`ptr`),
  `cstruct` for by-value C structs (with nested aggregates), opaque handles
  (`ptr` and empty-body `cstruct`), and multi-`link`/`cflags`. All four embedded
  code examples verified to actually compile and run correctly. See issue #147.

## v0.102.0

- **CI: added a workflow that actually runs the test suite.** `release.yml` (build +
  cross-compile + publish) never ran `go test` — a broken compiler could ship to
  users. Added `ci.yml` (push to `main` + every PR): build, `go test ./...`, and
  `./examples/run.sh` as an integration smoke test. `release.yml` now runs `go test
  ./...` before cross-compiling, so a tag with failing tests does not publish a
  release. See issue #85.

## v0.101.0

- **Test: covered the `machin encode` command path.** `splitFunctions`, `stripLineComment`,
  and `cmdEncode`'s core (`composeSources`) had no direct tests despite being the
  project's primary authoring entry point (`framework/run.sh` depends on it to build
  every framework app). Added: nested-brace splitting and the "unbalanced braces"
  error path for `splitFunctions` (previously only its duplicated copy in
  `splitFunctionsLoc` had a test); a table-driven `stripLineComment` test (trailing
  comment removed, `//` inside a string literal preserved); multi-file concatenation
  order, a `loadMFL` round-trip, and type-error surfacing for `composeSources`. See
  issue #82.

## v0.100.0

- **Fixed: `examples/complex/json_echo_api.mfl` silently truncated large/split POST
  bodies.** `read(fd)` is one `read(2)` syscall, returning at most 65535 bytes — not a
  whole HTTP request. `framework/machweb.src`'s own server (`read_request_bytes`) has
  looped correctly on this for a while; this specific example still used a single
  `read(conn)` and had no test proving otherwise. Fixed by inlining the same
  loop-until-`Content-Length` pattern (`read_bytes` + `bytes_index` + a small
  `content_length` helper), kept self-contained (no framework composition needed).
  Verified against a real 100 KB body: the old code silently truncated to 65,353
  bytes; the fix round-trips all 100,000. `docs/LANGUAGE.md` and `SPEC.md` now
  document the single-read limitation next to `read`/`write`, pointing at the correct
  pattern. New `TestReadBytesLoopReassemblesLargePayload`. See issue #91.

## v0.99.0

- **Fixed: duplicate function definitions were silently accepted (last wins).** Two
  functions with the same name compiled with no error — the checker's registration
  loop just overwrote the earlier entry in its lookup map. A real footgun for the
  framework composition model (`machin encode framework/machweb.src app.src`), which
  puts the framework and the app in one shared global namespace: an app name
  colliding with a framework name (`route`, `param`, `serve`, …) silently won or lost
  depending on declaration order, with zero diagnostic. `Check` already rejected
  duplicate *types* this way; functions had no equivalent guard. Now a clear
  `duplicate function "name"` error (`machin check --json` code `duplicate-function`).
  Fixing this surfaced a real latent collision already shipping in the repo:
  `framework/example.src`'s `route(req)` collided with `machweb.src`'s
  `route(router, method, path, handler)` — renamed to `handle_request`, and
  `framework/README.md` now documents the shared-namespace rule explicitly. See
  issue #88.

- **Added: `machin test [--json] <src...>` — a native MFL test runner (#236 Stage A).**
  `framework/test.src` provides `assert(cond, msg)`, `assert_eq_int(got, want, name)`,
  `assert_eq_str(got, want, name)`, and `test_summary()` (call last in `main()` — prints the
  `TEST_SUMMARY passed=N failed=M` tally, exits 1 on any failure). `machin test` composes
  `framework/test.src` ahead of the given sources — the same multi-file compose `machin
  encode` already does, so testing a framework module means passing it alongside its test
  file (`machin test framework/flags.src framework/tests/flags_test.src`) — builds the
  result as one program, runs it, and reports the tally (`--json`: `{ok, passed, failed,
  files}`). Lets framework/app tests be written and run in MFL directly, without the Go
  harness (`RunCaptured`); the Go test suite (`go test`) remains the compiler's own layer.
  Stage B (real per-line `.src` coverage) is deferred, per the issue's own staged plan.

## v0.98.0

- **Fixed: `read_file`/`read_file_bytes` segfaulted on a directory path.** `fopen(dir,
  "rb")` succeeds on Linux (opening a directory read-only is legal at the syscall
  level), but `ftell()` on the resulting stream returns `LONG_MAX` — not `-1` — so the
  existing "if (n < 0) n = 0" guard never caught it, and the runtime tried to allocate
  ~9.2 exabytes for the "file size." A very easy path to hit in practice: `list_dir()`'s
  entries can themselves be subdirectories, so `read_file_bytes` on any of those crashed
  — found building a concurrent file-hasher demo (`machin-hasher`) that reliably
  segfaulted on any target directory containing so much as one subdirectory, even
  single-threaded (not a race — a plain crash). Fixed with an explicit `stat()`-based
  directory check before `fopen`; both builtins now return empty (matching their
  existing "can't open" behavior) instead of crashing. New `TestReadFileOnDirectory`.

## v0.97.0

- **`framework/smtp.src` gained STARTTLS support, closing issue #260's SMTP half.**
  `smtp_send` takes a new `use_tls` argument: after `EHLO`, it sends `STARTTLS`, upgrades
  the connection in place via `tls_client_fd` (shipped v0.92.0), and re-issues `EHLO` over
  the now-encrypted channel per RFC 3207 (capabilities can change post-upgrade, and
  skipping this would let a MITM strip the plaintext EHLO unnoticed) — the rest of the
  session (AUTH/MAIL/RCPT/DATA/QUIT) then runs over the TLS handle. New `smtp_write`/
  `smtp_close` helpers dispatch to the TLS handle or the plain fd depending on whether the
  session was upgraded. A real submission relay that *requires* TLS (Gmail/SendGrid/SES on
  `:587`) is now reachable; a local catcher or a relay that accepts plaintext submission
  still works with `use_tls=0`. Verified two ways: `machin-mail`'s new `--starttls 1` flag
  against a genuinely live `smtp.gmail.com:587` — the full plaintext dance, the upgrade, and
  the post-upgrade `EHLO` all succeed, failing only at the expected, auth-gated `MAIL FROM`
  step (no real credentials supplied) — and a new `TestSMTPSendStartTLS` against a local Go
  SMTP-shaped server, confirming an untrusted/self-signed cert is correctly **rejected**
  (verification is active, not disabled). Hit MFL's flat-function-scope gotcha along the
  way: `smtp_close`'s two branches return different types (`tls_close`'s `int` vs `close`'s
  void), so neither result is captured into a shared variable.

## v0.96.0

- **Self-hosted concurrency: the MFL-in-MFL compiler now compiles `go`/channels/
  `select` — and proves them race-free (#280).** The self-hosted compiler
  (`selfhost/*.src`) gained byte-identical codegen for goroutines (a per-site
  arg-struct + trampoline + detached `pthread_create`), scalar/string/struct channels
  (`make`/`send`/`recv`, the `offsetof` string-copy path), `range` over a channel, and
  `select` (checker + poll-loop). **All five concurrency corpus apps (healthcheck,
  linkcheck, pipe, pool, wscat) now self-host byte-for-byte** — verified by
  `verify-cgen.sh` (411 PASS/0 FAIL incl. fuzz + self-application). On top of that, the
  inferred **data-race analysis** was ported into the self-hosted compiler
  (`selfhost/racecheck.src`, oracle `machin racetest --program`): local parameter races
  (type-aware reachability), happens-before precision (live-counting + channel-join
  barriers + loop multiplicity), package globals (unconditional sharing), and
  move-on-send (use-after-move) — `verify-race.sh` 22 PASS/0 FAIL, byte-identical to the
  reference. So machin-in-machin now both compiles concurrent programs *and* infers their
  race-freedom. Only slice/map (JSON) channels remain of #280 codegen — unused by the
  corpus.

## v0.95.0

- **Fixed: `machin run`/`build` rejected hand-written, ordinary-looking MFL
  with a misleading "illegal base64 data" error.** Found by dogfooding: 5
  parallel demos were built to stress the least battle-tested recent features
  (`serve_tls`, `--static` + `serve_tls` + SQLite combined, `tls_client_fd`
  against a real SMTP server, `--race-safe` against a deliberately-introduced
  bug, secp256k1/keccak256 outside EIP-712) — every one of those *features*
  came back clean, but **3 of 5 agents independently hit the same friction**:
  writing a normal multi-line, Go-like-formatted `.mfl` file (the natural way
  to write code) and running it directly failed, because `loadMFL` only
  understood canonical one-declaration-per-line text or the packed base64
  form — a line like `println(x)` has no whitespace, so it looked like a
  malformed packed declaration instead of a fragment of a multi-line function
  body. `machin check` already tolerated this shape (via the same
  encode-style `splitFunctionsLoc`/`normalize` machinery `machin encode`
  uses) — `run`/`build` now do too, closing the inconsistency rather than
  just improving the error message. New `TestLoadMFLAcceptsLooseSource` /
  `TestLoadMFLStillAcceptsCanonicalAndPacked`; the whole `examples/` corpus
  (39 files) re-verified with zero regressions.
- **Fixed: `serve`/`serve_tls`'s startup banner silently vanished under any
  redirected/piped/daemonized deployment** (`nohup`, systemd, Docker,
  `&> log`) — `println` before the accept loop was never followed by
  `flush()`, so it sat in libc's pipe buffer forever (the accept loop never
  returns to flush it later). Also found via the dogfood demos. One-line fix
  in both `serve` and `serve_tls` (`framework/machweb.src`).
- **Fixed a stale gotcha**: `machin guide`'s `no-tls-without-https` still said
  "there is no raw TLS socket," directly contradicted by `tls_client_fd`/
  `tls_server_ctx`/`tls_accept` (shipped two releases ago, documented in the
  `server-tls-v1` gotcha two entries below it). Found via the same dogfood
  exercise — an agent skimming gotchas top-to-bottom could be misled into
  thinking STARTTLS isn't possible.

## v0.94.0

- **Fixed: `a < -b` failed to parse after its own canonical form (issue #208).**
  `machin encode` tightens whitespace around operators, so `x < -1` becomes
  byte-adjacent `x<-1` — and the lexer greedily merged that into a single
  channel-receive `<-` token, so a valid comparison against a negative literal
  failed to parse *after* the round trip through canonical form (the exact
  loop-integrity contract — generate, canonicalize, re-ingest — an agent runs
  constantly). Since `ch <- v` (a genuine channel send) is lexically the
  identical shape (an identifier immediately followed by `<-`), this couldn't
  be fixed at the lexer: whether `IDENT <- ...` means "send" or "less-than,
  then negate" depends on grammatical position, not local token context. Fixed
  in the parser instead: the one call site that recognizes send statements
  (a simple statement's leading expression) still stops at a bare `<-` as
  before; everywhere else (`if`/`while` conditions, assignment/return values,
  call arguments, nested sub-expressions) now reinterprets an unexpected `<-`
  found while precedence-climbing as `<` followed by unary `-` on the next
  operand — exactly how the un-tightened source already parsed. Verified:
  the reported case, operator-precedence preservation (`a && b < -1` parses as
  `a && (b < -1)`, not `(a && b) < -1`), composition with further arithmetic
  (`c < -1 + 10`), and non-regression on channel send/receive/select (the
  exact same ambiguous token shape, still correctly recognized). New
  `TestLessThanNegative` / `TestLessThanNegativeChannelSendUnaffected`.
  **Follow-up, not done here:** the self-hosted parser (`selfhost/parse.src`)
  has its own independent precedence-climbing implementation with the same
  `<-` handling shape, so it likely has the identical bug — left for a
  separate change to avoid colliding with the in-flight self-hosted
  concurrency-parity work (issue #280).

## v0.93.0

- **`machin guide`'s `proof` section gains a 5th entry: data-race safety —
  closing the gap where the project's flagship differentiator (v0.91.0's
  inferred data-race freedom) had prose docs but no reproducible benchmark,
  unlike everything else in that section.** New `bench/race-freedom`: the same
  textbook shared-counter race (4 threads, no sync, expected sum 8,000,000) in
  machin, Go, and Rust. `machin check` catches it at compile time on the
  untouched code (`RACE001`) and `--race-safe` refuses the build; Go's compiler
  accepts it silently (and visibly corrupted the output on this run — 3
  wrong numbers, confirmed a genuine bug via `go run -race`, not a false
  claim); Rust's naive translation fails to compile (`E0133`), and its actual
  safe fix needs `Arc<AtomicI64>`/`Ordering` wrapper types machin's
  zero-annotation channel-based fix doesn't need. The more useful finding than
  "it crashes": numeric output alone is NOT reliable evidence of a race either
  way — machin's racy build printed the correct sum on this run too, and so
  did Rust's `unsafe`-escaped version — which is exactly why compile-time
  detection matters more than hoping a test run happens to expose the bug.
  See issue #287.

## v0.92.0

- **Server-side TLS + STARTTLS — machweb can terminate HTTPS itself, no reverse
  proxy needed.** New builtins `tls_server_ctx(certfile, keyfile) -> int`,
  `tls_accept(ctx, fd) -> int` (server handshake), `tls_client_fd(fd, hostname)
  -> int` (the STARTTLS primitive — upgrade an already-connected, plaintext-
  negotiated fd to verified TLS in place), and `tls_read[_bytes]`/
  `tls_write[_bytes]`/`tls_close` (mirroring the plain fd read/write/close
  builtins over a tls handle). New `framework/machweb.src` function
  `serve_tls(port, certfile, keyfile, handler)` — `serve`'s TLS-terminating
  sibling, reusing the same router/handler/Response machinery; v1 scope is
  plain request/response only (`res.is_hijack`/`res.is_stream` get a 501 rather
  than misbehaving over a tls handle — a documented, deliberate limitation, not
  an oversight). Verified end-to-end: a real Go TLS client driving a full HTTP
  round trip through `serve_tls` and a router, and a STARTTLS upgrade correctly
  **rejecting** an untrusted/self-signed certificate (proving verification is
  active on the upgrade path too, same discipline as `--static`'s CA bundle
  work in v0.90.0). `tls_client_fd` shares `mfl_tls_dial_e`'s already-proven
  handshake code — the dial-vs-upgrade split is just where the fd came from.
  Closes the "you still need nginx in front of it" gap in the single-binary
  pitch; see issue #260 (STARTTLS wiring into `machin-mail` is a follow-up in
  that separate repo, not part of this change).

## v0.91.0

- **Inferred data-race freedom — Rust's guarantee, no `Send`/`Sync`.** `machin check`
  now runs a data-race analysis after typecheck (phase `race`), inferring — with **zero
  annotations** — which heap locations are shared *and* concurrently accessed across
  goroutine boundaries, at least one a write. Reported as errors with a counterexample:
  `RACE001` write/write, `RACE002` read/write, `RACE004` use-after-move (a value used
  after `ch <- v` transferred it). `machin build|run --race-safe` refuses to compile a
  program with an inferred race (plain `build` is unaffected). Covers locals across
  goroutines (reachability-based: a slice-field of a by-value struct still shares its
  backing; a scalar field doesn't), package globals (one shared cell — even a scalar
  global races), channel move-on-send, and closures captured into a `go`-spawned function.
  **Sound** (borrow-checker discipline: never a false negative on the covered surface),
  with happens-before precision so accesses before a spawn or after a channel-join barrier
  aren't flagged. Validated against the 5 concurrency corpus apps (0 false positives; a
  mutation test confirms it engages). New `racecheck.go` + `racecheck_test.go`; documented
  in `docs/check-json.md`, `docs/concurrency-race-freedom.md`, and `machin guide`.

## v0.90.0

- **`machin build --static` now works for TLS/crypto-using programs, FROM scratch.**
  The static OpenSSL link already worked (this host's `libssl-dev` ships static
  archives — no vendoring, no musl needed for this part); the missing piece was
  certificate verification with no system CA store. A public CA root bundle
  (Mozilla's, as shipped by Debian/Ubuntu's `ca-certificates`, ~245 KB) is now
  gzipped and embedded (`vendor/certs/`, same pattern as the SQLite amalgamation),
  compiled into static builds, and loaded into the `SSL_CTX` as a fallback
  alongside the system store. Verified in a genuinely empty `FROM scratch` Docker
  image: a real HTTPS request with full certificate verification succeeds, and a
  known self-signed/untrusted certificate is correctly **rejected** — proving
  verification is active, not disabled. New `bench/tls-static` (a 4th entry in
  `machin guide`'s `proof.benchmarks`): 26.5 kB dynamic vs **5.28 MB fully static**
  — a different, honest number from the libc-only 92.9 kB figure, not folded into
  it. New `TestStaticBuildBundlesCACerts`. Distinct from issue #260 (server-side
  TLS/STARTTLS — still open); this closes issue #283 (the packaging/FROM-scratch
  gap). Pair with the default `cc`, not `CC=musl-gcc` — the static OpenSSL here is
  glibc-built.

## v0.89.0

- **`machin guide` gained a `proof` section — the "why trust these claims" answer, as
  structured data an agent can read and relay, not a blog post a human has to find.**
  Schema bumped to `machin.guide/v3`. `proof.selfHosting` states the self-hosting fixpoint;
  `proof.benchmarks` carries the three measured comparisons (agent write cost vs Go/Python,
  native runtime speed vs Rust/Zig, cold-start/ship-size vs Node) that already lived in
  README/bench/, each with a `reproduce` command pointing at the actual `bench/` script — not
  asserted, re-derivable. `--text` renders a PROOF section. New `TestGuideProofReproducible`
  keeps every `reproduce` path honest against the repo, the same way existing tests keep
  builtins/idioms honest. Machin's positioning is machine-first, not human-DX — so the
  credibility mechanism is an agent researching "what should I build this in" reading
  `machin guide` and relaying the numbers to its principal, not a marketing page.

## v0.88.0

- **`keccak256` + secp256k1 signing — the primitives EIP-712/Ethereum-style signing
  needs.** New builtins: `keccak256(bytes) -> bytes` (Ethereum's hash — a self-contained
  Keccak-f[1600] sponge, distinct from NIST SHA3-256's padding), `secp256k1_pubkey(bytes)
  -> bytes` (65-byte uncompressed pubkey from a 32-byte private key), `secp256k1_sign_recoverable(bytes,
  bytes) -> bytes` (priv32, hash32 -> 65-byte r||s||v, EIP-2 canonical low-S, v as 27/28),
  and `secp256k1_recover(bytes, bytes) -> bytes` (hash32, sig65 -> the recovered pubkey,
  same math as Solidity's `ecrecover`). Implemented over OpenSSL's generic EC API
  (`NID_secp256k1`, already linked via `-lcrypto` — no new dependency); OpenSSL has no
  recoverable-ECDSA entry point, so the recovery id is derived by testing both y-parities
  of R and matching the resulting candidate pubkey, the standard technique for signers
  that don't link a dedicated secp256k1 library. Verified against the known-vector G point
  (priv=1), the two published Keccak-256 test vectors, and a signature produced
  independently by Python's `coincurve` (a real libsecp256k1 binding — a different
  codebase than OpenSSL, ruling out curve/recovery-math bugs a self-consistent
  OpenSSL-only round trip could hide). New `eip712-sign` guide idiom; a full EIP-712 ABI
  encoder is not a builtin (see the `eip712-uint256` gotcha — MFL's 64-bit `int` can't
  hold Solidity `uint256` fields, encode those as 32-byte `bytes` by hand). Driven by
  javika-machin's `printer`, whose Polymarket order signer was blocked on exactly this
  (MFL had `sha256`/`hmac_sha256` but no keccak/secp256k1).

## v0.87.0

- **`machin guide` now enumerates the CLI — a structured `commands` section.** The catalog
  gained a `commands` array (every subcommand — `run`/`build`/`encode`/`check`/`pack`/`guide`/
  `framework`/`skill` — with its usage and one-line purpose), so an agent can discover the whole
  tool surface, not just the language. Schema bumped to `machin.guide/v2`; `check` and `build`
  moved out of `gotchas` into `commands` (their correct home). `--text` renders a COMMANDS section.

## v0.86.0

- **`machin check [--json]` — agent-native diagnostics.** A machine-first alternative to a
  language server (which renders for a human): `machin check --json a.src` (or `--stdin`)
  runs lex → parse → typecheck **only — no codegen, no `cc`** (milliseconds), and returns
  the checker's verdict as structured data: `{ok, errorCount, diagnostics:[{severity,
  phase, code, message, decl, line, snippet}]}`, exit 0 iff clean. An agent in a
  write → check → fix loop branches on the stable `code` (`type-mismatch`,
  `undefined-name`, `arity-mismatch`, `parse-*`, `no-main`, …) instead of scraping error
  prose, and `decl` names the function to fix (the natural unit for a one-declaration-per-
  line language). Parse errors are reported per-declaration (multiple in one run, each with
  its source line); the typecheck phase reports one diagnostic (the checker bails on the
  first — v2 will accumulate). Pairs with `machin guide`: the surface + the verdict, both
  bulk JSON, no human in the loop. Spec: [`docs/check-json.md`](docs/check-json.md).

## v0.85.0

- **The no-Go bootstrap: machin is now written in machin, full stop.** v0.84.0 proved the
  *compiler* compiles itself; the toolchain around it (`encode`, the `build`/`run`
  orchestration, the CLI) was still Go. Those are now ported to MFL too, so the repo
  rebuilds the entire `machin` binary from its own source with **zero Go in the loop**
  (`selfhost/verify-nogo.sh`): a machin binary encodes + type-checks + compiles + links +
  runs machin, and the rebuild reproduces itself byte-for-byte (both the encoded source
  and the generated C). The standalone MFL `machin` (~248 KB) is a drop-in for the Go one
  across libc / crypto (OpenSSL) / SQLite / math / TLS / regex / xeddsa / raylib-FFI
  programs — byte-identical output, verified over the whole corpus. The runtime prelude is
  now feature-gated (only the blocks a program uses are emitted), matching the Go compiler
  exactly. Go remains one *replaceable* way to mint the seed binary — not "under the hood."
  (Remaining for 100% CLI parity: `select`/struct-channels/closures codegen, `--static`,
  the wasm target.) See [`selfhost/BOOTSTRAP.md`](selfhost/BOOTSTRAP.md) Stage 7.

## v0.84.0

- **The machin compiler now compiles itself (a compiler bootstrap).** The full
  compiler — lexer, parser, type checker, and C code generator — is now written *in
  MFL* (`selfhost/`, ~4k lines) and emits the same machine code as the reference Go
  compiler byte-for-byte. The **fixpoint** holds: the MFL compiler compiles its own
  source into a native binary (`mflc2`) that re-emits its own source identically
  (`mflc2.c == mflc3.c`), and its generated C matches the Go compiler for arbitrary
  programs. (This is compiler self-hosting, not a hosting/deployment feature.) Every
  stage was built against a byte-diff oracle (`machin lextest`/`parsetest`/`checktest`/
  `cgentest`), and the effort surfaced three real compiler bugs (below) plus two
  general runtime speedups. On the full parse→typecheck→codegen of its own source the
  self-hosted compiler runs ~0.9× the Go reference (competitive). See `selfhost/`.

- **`join([]string, sep)` is now O(n) instead of O(n²).** It built the result with
  repeated `mfl_cat` (each copying the whole growing string); it now does one length
  pass, a single allocation, and memcpy per piece. Building large strings by collecting
  chunks in a slice and `join`-ing once is now linear — the idiomatic fast string
  builder for MFL. (Surfaced by the self-hosted compiler emitting ~7k lines of C:
  switching its output accumulation from `s = s + chunk` to slice+join took the whole
  parse→typecheck→codegen pipeline from 7.4× slower than the Go compiler to ~0.9×.)

- **Fixed: string ordering comparisons (`<` `<=` `>` `>=`) compared pointers, not
  contents.** The type checker accepts ordering on strings (it returns bool), but
  codegen only routed `==`/`!=` through `strcmp` — the four relational operators fell
  through to raw C pointer comparison, so e.g. `"dbl" < "add"` could return true and
  sorting strings gave garbage. All six comparisons now go through `mfl_strcmp`.
  (Surfaced by the self-host type checker sorting its instance dump.)

- **`substr` is now O(1) per call on a repeated source (was O(string length)).**
  `mfl_substr` needed `strlen(s)` only to clamp the end offset; it now memoizes the
  length by pointer identity, so slicing one buffer in a loop (lexers, parsers,
  scanners, JSON/CSV readers) no longer rescans the whole string each time. Clamping
  behavior is unchanged (semantics-identical cache; the arena free path invalidates it
  so a reused address can't return a stale length). Measured: the self-host lexer over
  the corpus dropped 8.3× (5283 → 636 ms), from 19× off hand-written Go to ~1.4×.
  Surfaced by the bootstrap (`selfhost/PERF.md`, `selfhost/bench.sh`).

## v0.83.0

- **New builtin `exec(cmd) -> (exit_code, stdout, stderr)`** — run a shell command and
  capture its output (unlike `system`, which returns only the exit code). Runs via
  `/bin/sh` in a subshell with stdout/stderr redirected to temp files (no pipe-buffer
  deadlock); multi-assign only (`code, out, err := exec(...)`). Unblocks SSH / mongodump
  / gzip pipelines and any tool whose output you need — the gap from #277 (port
  mongo-vault to MFL). Captured text is NUL-terminated; redirect binary output to a file
  in the command.

- **Claude Code plugin marketplace.** The repo is now a Claude Code marketplace
  (`.claude-plugin/marketplace.json`) shipping a `machin` plugin that bundles the 5
  agent skills (machin-start + web/backend/gamedev/deploy). Install in any project:
  `/plugin marketplace add javimosch/machin` then `/plugin install machin@machin` — so
  an agent reaches for machin at the decision moment *everywhere*, not just where
  `machin skill install` was run. Bundled skills are kept identical to the canonical
  `skills/` (the embedded source of truth) by `tools/sync-plugin-skills.sh`, guarded by
  a test; manifest validated with `claude plugin validate`.

- **wasm: unbuffer stdout/stderr.** A `wasm32-wasi` reactor module has no `exit()` to
  flush stdio, so output from an exported function was lost on return (only the first of
  several `println`s came through). A constructor now sets stdout/stderr unbuffered for
  the wasm target at `_initialize`; native keeps normal buffering. Surfaced building the
  browser playground.

## v0.82.0

- **The framework modules now ship in the binary.** `machweb.src`, the DB drivers,
  `sso`/`ws`/`smtp`/`reactive`/`router`/`flags`/`bson` — the MFL libraries an app
  composes against — are `//go:embed`'d, and `machin encode` resolves
  `framework/<name>.src` from that embed when the local file is absent. So
  `machin encode framework/machweb.src app.src` **works on a bare binary-only install**
  (curl|sh), not just inside a repo checkout. A local copy still wins (vendoring is
  respected). New `machin framework list | <name> | --vendor [dir]` for explicit
  access. **Found by a clean-room re-validation:** in a fresh container the quickstart's
  first command failed (`no such file: framework/machweb.src`) — the prior "loop
  validated" was a false positive from running on a machin-saturated machine. Re-run in
  the clean room now goes end-to-end: bare box → embedded framework → `--static` →
  a working FROM-scratch SQLite binary.

## v0.81.0

- **New `machin build --static` — a fully static binary that bundles SQLite.** The
  SQLite [amalgamation](vendor/sqlite/) (public domain, v3.53.3) is embedded in the
  compiler (gzipped, `//go:embed`) and compiled directly into a program in `--static`
  mode, so a **SQLite-using app links no `libsqlite3`** and, paired with `CC=musl-gcc`,
  produces a **libc-free ~1 MB binary that runs `FROM scratch`** (verified: a
  `bench/rest-sqlite` build → a 1.23 MB scratch image serving SQLite-backed traffic).
  This unblocks the most common backend shape (REST+SQLite) for zero-dependency
  deploys — the gap the flagship + the adoption-loop validation both surfaced. TLS/
  crypto (OpenSSL) is not bundled (`--static` warns and the link needs static OpenSSL);
  native TLS that removes the OpenSSL dependency is tracked in issue #260. The
  `machin-start` / `machin-deploy` skills now document the turnkey path.
- **New embedded skill `machin-start`** (`machin guide --skill start`) — the
  agent-facing entry point that comes *before* the domain how-tos: when to reach
  for machin (and when not), with the measured `bench/` numbers as decision
  criteria, plus a zero→running→shipped quickstart. Listed first in the
  `machin guide` domain registry.
- **New `machin skill install` command** — closes the skill-discovery loop: writes
  the embedded SKILL.md files where coding agents actually look. By default the
  vendor-neutral `~/.agents/skills/` **plus any detected runtime** (Claude Code's
  `~/.claude/skills/`, the same SKILL.md format), so machin surfaces at the decision
  moment in a real session — not only inside this repo. Works regardless of how
  machin was installed (curl|sh *or* source build), unlike the old install-time-only
  path. `machin skill list` / `machin skill show <name>` / `--dir <path>` for a
  project. `install.sh` now calls it (opt out `MACHIN_NO_SKILL=1`).
- **Fix: rename the `bench/cold-start` Dockerfiles off the `.go` extension**
  (`Dockerfile.go` → `go.Dockerfile`, etc.) so `go vet ./...` / `go test ./...` no
  longer try to parse a Dockerfile as Go.
- **Fix: `machin-start` skill overpromised the `FROM scratch` static story.** An
  adoption-loop validation (a fresh agent given a deploy task with no mention of
  machin *did* reach for it via the skill and shipped a working binary — loop
  confirmed) surfaced that the quickstart implied the REST+**SQLite** app ships
  `FROM scratch` via a one-line musl wrapper. It doesn't — `musl-gcc` can't find
  `sqlite3.h`; a SQLite/TLS app needs its dep compiled in statically (the SQLite
  amalgamation / static OpenSSL). The skill now states two honest ship paths: the
  default small **dynamic** binary on a slim base, and true static for pure-compute
  (where the 92.9 kB figure actually holds).

Driven by **machin-wiki** (local) — the first wasm client that *initiates* server calls
through returning `extern "env"` imports (`data := http_get(url)`, used inline), with
local PBKDF2 auth and SPA routing:

- **New builtin: `pbkdf2_sha256(password, salt, iterations, dklen) -> bytes`.**
  PBKDF2-HMAC-SHA256 via OpenSSL's `PKCS5_PBKDF2_HMAC` (linked `-lcrypto`, same pattern
  as the other crypto builtins). The password-hashing primitive MFL was missing — local
  auth in a self-hostable web app can't lean on SSO alone. Tested in `mfl_test.go`.
- **Wasm target: gate the POSIX headers that newer wasi-libc `#error`s out.** `sys/wait.h`
  and `signal.h` were unconditionally `#include`d in the core C runtime; zig's shipped
  wasi-libc now emits `#error "wasm lacks signal support"`, which broke **every** wasm
  build (even `export func start(){}`). Gated behind `#ifndef __wasm__`, and `mfl_system`
  (uses `WEXITSTATUS`) likewise. Net/TTY runtimes were already pay-as-you-go.
- **Learnings recorded** (in `skills/machin-web/SKILL.md` Gotchas + a new *Returning effect
  imports* how-to): returning `extern "env"` imports work end-to-end BUT the `alloc` builtin
  isn't exported, so the host needs a `func alloc_export(n) (p) { p = alloc(n) }` export to
  write the response string; `json_get` paths don't compose `key[idx].field` → use typed
  `parse(body, Struct{})`; reactive signals are int-only; named functions can't be passed
  by value; SPA catch-all server routing; `--virtual-time-budget` for deep-link tests.

## v0.80.0

- **An SMTP toolkit — `framework/smtp.src`.** Send mail and receive it, both pure MFL over
  `dial`/`listen` + read/write, no library, no cgo:
  - **client** — `smtp_send(host, port, from, to, subject, body, user, pass) -> (ok, errmsg)`
    runs the full `220`/`EHLO`/`AUTH LOGIN`/`MAIL`/`RCPT`/`DATA`/`QUIT` conversation, with
    `base64` AUTH, multiple recipients, and dot-stuffing.
  - **server** — `smtp_recv(conn) -> (Mail, ok)` plays the receiving side of one session
    (a catcher), plus a buffered line reader (`line_reader`/`read_line`/`read_reply`) and
    `mail_header`/`mail_body` parsers.
  - Plaintext SMTP + AUTH (enough for a relay that doesn't force TLS, and for a local
    catcher). STARTTLS/implicit TLS is the next step (it needs a wrap-an-fd-in-TLS
    primitive). No new builtin — it rides `dial`/`read_bytes`/`base64_encode`.
- Dogfooded by **[machin-mail](https://github.com/javimosch/machin-mail)** — a self-contained
  SMTP toolkit binary: `send` mail and `sink` it (a local catcher + a web inbox, à la
  MailHog/Mailpit), so the sender is testable with **zero external dependencies**. Verified
  both directions against independent standard implementations (Python `smtplib` ⇄ the
  machin client/sink).

## v0.79.0

- **A WebSocket *server* (RFC 6455) for machweb** — `framework/ws.src`, the symmetric half
  of the existing wss client. A handler returns `ws(req, fn)` and machweb hands over the
  raw socket (new generic `hijack(fn)` response: write nothing, run `fn(conn)`); `fn` does
  the upgrade handshake (`Sec-WebSocket-Accept = base64(sha1(key + GUID))`) and speaks
  frames. The frame codec is **pure MFL over bytes + the bitwise builtins**: unmasked
  server frames out (`ws_send_text` / `ws_send_bytes` / `ws_send_close` / 7-, 16-, 64-bit
  lengths), masked client frames in (`ws_recv` unmasks via the 4-byte XOR key),
  `ws_next_text` transparently answers pings and stops on close. Verified against the
  Python `websockets` reference client (handshake, unicode, fragmented lengths, ping/pong).
- machweb gains **`is_hijack`** on `Response` (hand the raw connection to a closure for any
  protocol upgrade) — no new builtin needed; the codec rides existing `bytes`/bitwise ops
  + `sha1_bytes`/`base64_encode_bytes`.
- Dogfooded by **[machin-rooms](https://github.com/javimosch/machin-rooms)** — a
  self-hostable real-time chat server: multi-room, broadcast fan-out, join/leave + live
  member count, two goroutines per connection (a reader + a writer over the shared fd),
  all in one MFL binary.

## v0.78.0

- **Production hardening + reverse-proxy awareness in machweb.** A machin app usually
  runs behind a proxy (nginx / Caddy / Traefik / Cloudflare) that terminates TLS — these
  make it correct and safe there. All default **off**; turn them on in `main()` (or call
  `harden(max_body_bytes, read_timeout_ms)` for the common set):
  - **Proxy-awareness** — `scheme(req)` / `client_ip(req)` / `base_url(req)` read
    `X-Forwarded-Proto` / `X-Forwarded-For` (only when `set_trust_proxy(1)`), so redirects,
    OAuth `redirect_uri`s, emailed links, and logged client IPs are right behind a proxy.
    `set_secure_cookies(1)` marks cookies `Secure`. `req.remote` is the raw socket peer.
  - **Hardening** — `set_max_body(n)` rejects an over-cap body with `413` *without*
    buffering it; `set_read_timeout(ms)` caps a slow client's request read (anti
    slow-loris); `set_access_log(1)` emits one JSON access-log line per request on stderr.
- **Two net builtins**: `peer_addr(fd)` (the socket peer IP via `getpeername`) and
  `socket_timeout(fd, ms)` (cap blocking recv/send via `SO_RCVTIMEO`/`SO_SNDTIMEO`).
- Dogfooded by **[machin-deploy](https://github.com/javimosch/machin-deploy)** — a
  reference production-ready machin service (proxy-correct, hardened, with a systemd unit,
  a slim Docker image, and nginx/Caddy snippets). New `machin guide --skill deploy`.

## v0.77.0

- **Streaming responses in machweb (Server-Sent Events).** A handler can now return
  `sse(fn)` / `stream_response(status, ctype, fn)` instead of a whole `Response`: machweb
  writes the headers (no `Content-Length`), then hands `fn` the connection so it writes
  the body **incrementally over a long-lived socket** — live logs, metrics, progress, LLM
  tokens. Helpers `sse_data(conn, msg)` / `sse_event(conn, name, msg)` /
  `sse_comment(conn, keepalive)` format SSE frames and return the write result (`< 0`
  once the client has gone, so a producer loop breaks cleanly). Normal (whole-Response)
  handlers are unchanged — streaming and normal routes coexist on one port.
- **SIGPIPE is ignored** in every binary's `main`, so writing to a peer that closed the
  connection (an SSE client that navigated away) returns `-1`/`EPIPE` instead of killing
  the process. (A latent hazard for any long-lived server, not just streaming.)
- Dogfooded by **[machin-live](https://github.com/javimosch/machin-live)** — a
  self-hostable live event/log stream hub: producers `POST /push/<topic>`, browsers watch
  `GET /stream/<topic>` over SSE, fanned out across goroutines by a single hub goroutine
  that owns the subscriber set (the idiomatic lock-free channel pattern — a `chan` inside
  a struct, a `[]Sub` registry).

## v0.76.0

- **File uploads in machweb (multipart/form-data).** Requests are now read **binary-safe**
  — `read_request_bytes`/`parse_request_bytes` keep the body as raw bytes
  (`req.body_bytes`), so an upload with NUL bytes survives intact (the old `read()` path
  truncated at the first NUL). New `parse_multipart(req)` splits a `multipart/form-data`
  body into its `MultipartPart`s (fields + files), with `multipart_file(req, field)` and
  `multipart_field(req, name)` convenience helpers. Responses gain extra headers via
  `with_header(res, name, value)` (e.g. `Content-Disposition` for download filenames).
- **Three enabling builtins**: `bytes_index(haystack, needle, from) -> int` (NUL-safe
  byte search, for binary protocols / multipart boundaries), `write_file_bytes(path,
  bytes) -> int` (binary-safe file write, for storing uploads), and the binary request
  path above. `req.body` (text view) is unchanged for existing apps.
- Dogfooded by **[machin-share](https://github.com/javimosch/machin-share)** — a
  self-hostable file/paste drop in one MFL binary (the upload/download/streaming surface
  that surfaced all of the above).

## v0.75.0

- **A `machin-backend` skill** — the backend domain now has its own how-to (the five
  pooled datastores + the uniform JSON-rows/`parse` idiom, connection pooling, signed
  sessions + SSO, the agent-first CLI contract, daemons, build/verify, gotchas). Embedded
  like the others: `machin guide --skill backend`. The `backend` domain in `machin guide`
  now routes to it.

## v0.74.0

- **Domain how-tos in the binary.** `machin guide` now leads with a **DOMAINS** section
  routing an agent to the right per-domain how-to (web / gamedev / backend), and the
  web + gamedev SKILLs are **embedded** — `machin guide --skill web` / `--skill gamedev`
  print the full guide offline (no repo checkout needed). Closes the gap where an agent
  with only the curl|sh binary reverse-engineered the language from demo repos. The JSON
  guide gains a `domains` array. Web skill now points at the networked DB drivers; both
  skill descriptions refreshed to current coverage.

## v0.73.0

- **MySQL connection pooling** — the MySQL/MariaDB client is now handle-based
  (`MySQLConn`) with an async-channel pool, like the other drivers:
  `mysql_pool_init(n, host, port, user, pass, db)` then `mysql_acquire()` + `myq`/`myx`
  + `mysql_release(c)`. The global `mysql_*` API is unchanged. Verified: 30 goroutines
  over 4 connections, all correct. **All five datastore drivers (Postgres/Redis/Mongo/
  MySQL) are now poolable** (SQLite is embedded).

## v0.72.0

- **MySQL / MariaDB client** (`framework/mysql.src`) — a pure-MFL client over the wire
  protocol, no libmysql. `mysql_connect` (mysql_native_password / SHA-1 challenge auth),
  `mysql_query` (text protocol → a JSON-array-of-rows string, numeric columns unquoted so
  `parse(rows, []T{})` decodes), `mysql_exec` (→ affected rows), `mysql_escape`,
  `mysql_close`. Verified against `mariadb:11`.
- **`sha1_bytes(bytes) -> bytes`** — SHA-1 digest (OpenSSL), for legacy auth like MySQL's
  native password. This unblocked the MySQL handshake.

## v0.71.0

- **MongoDB connection pooling** — the driver is now handle-based (a `MongoConn` =
  fd + read-buffer box) with an async-channel pool, mirroring the Postgres/Redis
  clients. `mongo_pool_init(n, host, port, user, pass)` once, then `mongo_acquire()` /
  the `m*` handle ops (`mins`/`mfind`/`mfindall`/`mfindid`/`mdel`/`mdelid`/`mcount`/
  `mdrop`/`mauth`/`mcmd`) / `mongo_release(c)` per request — so a concurrent server
  (machweb's per-request goroutines) never interleaves on one connection. The global
  `mongo_*` API is unchanged (thin wrappers over one default connection). Verified:
  30 goroutines over 4 pooled connections, every result correct.

## v0.70.0

- **MongoDB: query by `_id` (ObjectId) + filtered finds/deletes.** `bson_oid(acc, key,
  idhex)` encodes an ObjectId from its 24-char hex (the form the decoder produces), so
  you can query by `_id`. New driver helpers: `mongo_find(db, coll, filter)` (an explicit
  BSON filter), `mongo_find_by_id`, `mongo_delete(db, coll, filter)`, `mongo_delete_by_id`.
  `mongo_find_all` is now `mongo_find` with a match-all filter. Closes the ObjectId gap
  the machin-cms dogfood hit (get/delete a document by its id).

## v0.69.0

- **`system(string) -> int`** — run a shell command, returning its exit code (-1 if
  unlaunchable). For process orchestration — e.g. a CLI spawning a detached daemon
  (`system("./app serve >log 2>&1 &")`). Surfaced by the machin-cms dogfood (a
  daemon start/stop mode).

## v0.68.0

- **`parse_float(string) -> float`** — parse a floating-point number (strtod; 0.0 on
  non-numeric), the float counterpart to `parse_int`. Lets a tool turn a textual number
  (a SQLite REAL read as JSON, a CLI arg) into an MFL float — e.g. carry REAL columns
  into Mongo doubles in `machin-db-migrate`.

## v0.67.0

- **MongoDB client v2** (`framework/mongo.src` + `bson.src`): **SCRAM-SHA-256 auth**
  (`mongo_auth(authdb, user, password)` — the SASL conversation via `saslStart`/
  `saslContinue`, same SCRAM math as the Postgres client), **doubles** (BSON 0x01,
  encode + decode), and **cursor pagination** — `mongo_find_all` now follows the cursor
  with `getMore`, so it returns *all* documents, not just the first batch. Also adds
  BSON binary (used for the SCRAM payloads). Verified against an authenticated `mongo:7`
  (login + a double + 250 docs across batches).
- **`f64_bits(float) -> int` / `f64_from_bits(int) -> float`** — reinterpret a double's
  IEEE-754 bits as an int64 and back, the byte-level access needed to (de)serialize
  64-bit floats (e.g. BSON doubles). This unblocked Mongo doubles.

## v0.66.0

- **MongoDB client** (`framework/mongo.src` + `framework/bson.src`) — a pure-MFL
  client speaking the **OP_MSG wire protocol** over `dial()`, with a **BSON codec**,
  no driver and no cgo. `bson.src` builds a document (`bson_new` + `bson_str`/`bson_i32`/
  `bson_i64`/`bson_bool`/`bson_null`/`bson_subdoc`/`bson_subarr` + `bson_finish`) and
  decodes one to a JSON string (`bson_to_json`). `mongo.src`: `mongo_connect`,
  `mongo_insert_one`, `mongo_find_all` (→ a JSON-array string, so `parse(docs, []T{})`
  decodes; `_id` ObjectId comes back as a hex string), `mongo_count`, `mongo_drop`,
  `mongo_close`. Verified against `mongo:7`. v1: unauthenticated local Mongo,
  int/string/bool/null/ObjectId (doubles decode to null), first-batch finds.

## v0.65.0

- **Connection pooling for the Postgres + Redis clients** — the gap the SaaS demo
  surfaced (machweb runs each request in its own goroutine, so a shared single
  connection would interleave). The clients are now **handle-based**: a connection is
  a `PgConn`/`RedisConn` (an fd + a one-element read-buffer box that persists across
  calls though the struct is a value). A **pool** is an async channel of authenticated
  fds (machin channels are unbounded queues → a natural semaphore, no new language
  feature needed): `pg_pool_init(n, …)` / `pg_acquire()` / `pgq`/`pgx`/`pg_release`,
  and `redis_pool_init(n, …)` / `redis_acquire()` / `r*` helpers / `redis_release`.
  Each acquired connection has its own buffer, so concurrent requests never interleave.
  The single-connection API (`pg_connect`/`pg_query`/`pg_exec`; `redis_connect`/
  `redis_set`/…) is unchanged (thin wrappers over one default connection). Verified:
  30 goroutines over 4 pooled connections, every result correct (gated tests); the
  [SaaS demo](https://github.com/javimosch/machin-saas-demo) handles 40 concurrent
  requests cleanly.

## v0.64.0

- **Redis client** (`framework/redis.src`) — a pure-MFL Redis client speaking the
  RESP protocol over `dial()`, no client library. `redis_connect` (+ `redis_auth`),
  typed helpers (`redis_set`/`redis_setex`/`redis_get`/`redis_del`/`redis_exists`/
  `redis_incr`/`redis_expire`/`redis_rpush`/`redis_lpush`/`redis_lrange`/`redis_keys`),
  and a general `redis_cmd(args []string) -> (val, ok)` (ok=0 on a `-error`/nil).
  Parses all five RESP types; array replies come back as a JSON-array string, so
  `parse(val, []string{})` decodes them. For cache, sessions, counters, rate-limits,
  and simple queues. Verified against `redis:7`, plus a CI-runnable RESP test against
  an in-process canned-reply mock. v1 is string values.

## v0.63.0

- **SSO — OAuth2 / OpenID Connect** (`framework/sso.src`): "log in with Google /
  Microsoft / …" in pure MFL on top of machweb's signed sessions. Fill an
  `OAuthProvider`, then `sso_begin(p, secret)` (302 to the provider + a signed
  CSRF-state cookie) and `sso_complete(p, secret, req) -> (profile, ok)` (verify
  state, exchange the code via `http_request`, fetch the userinfo JSON). Identity
  comes from the userinfo endpoint, so no JWT/RSA is needed. Verified end to end
  against an in-process mock IdP (token exchange + userinfo + CSRF rejection).
- **machweb: `redirect(url)`** (302 + `Location`) and **`query(req, name)`** (a
  `?`-query-string parameter, url-decoded) — both general-purpose, needed by SSO.
- **Compiler fix — an omitted string struct-literal field is now `""`, not a NULL
  pointer.** A string's zero value is `""`, but C zeroes an omitted compound-literal
  field to `NULL`, which crashed every string op (compare/concat/len/substr/print).
  `structLit` now fills omitted string fields (recursing nested structs) with `""`,
  and the string operators (`+`, `==`/`!=`) are NULL-tolerant as defense. Surfaced by
  the SSO dogfood: a machweb handler returning a `Response` with an unset `location`
  segfaulted on `!= ""`. Regression tests added.

## v0.62.0

- **Cookies + signed sessions in machweb** (`framework/machweb.src`) — the auth
  foundation. Request: `cookie(req, name)` reads a request cookie. Response (a value
  type — the helpers return a new `Response`): `set_cookie` / `clear_cookie` add a
  `Set-Cookie` with safe defaults (`Path=/; HttpOnly; SameSite=Lax`). Login sessions:
  `set_session(res, secret, name, value)` stores `value` + an HMAC-SHA256 tag the
  client can't forge, and `get_session(req, secret, name) -> (value, ok)` returns
  `ok == 1` only if it verifies (rejects a tampered tag or the wrong secret). The
  `Response` struct gained a `cookies []string` field; `machweb_handle` emits the
  `Set-Cookie` lines. Keep `secret` server-side; the value is signed, not encrypted.

## v0.61.0

- **Postgres parameterized queries** — `pg_exec(sql, []string params)` in
  `framework/postgres.src` runs the **extended query protocol**
  (Parse/Bind/Describe/Execute/Sync): the SQL uses `$1, $2, …` placeholders and
  params are bound server-side as text, never interpolated — **injection-safe**.
  Works for `SELECT` (returns JSON rows like `pg_query`) and for
  `INSERT`/`UPDATE`/`DELETE` (returns `[]`). `pg_query(sql)` remains for trusted/
  constant SQL. Verified against `postgres:16`, including an injection param that
  stays inert. This was the top backend-roadmap follow-up to the v0.60.0 client.

## v0.60.0

A pure-MFL **PostgreSQL client** — the first networked datastore, and the start of
the SME-backend dogfood (see `docs/NORTH-STAR-BACKEND.md`). Building it surfaced two
binary primitives, now added to the language.

- **`framework/postgres.src`** — a PostgreSQL client speaking wire protocol v3 over
  `dial()`, with **SCRAM-SHA-256** auth (the modern Postgres default), in pure MFL
  (no libpq, no cgo). `pg_connect(host, port, user, db, password)` / `pg_query(sql)` /
  `pg_disconnect()`. `pg_query` returns a **JSON-array-of-rows string in the same
  shape as `sqlite_query`**, so `parse(rows, []T{})` decodes it — numeric and bool
  columns come back unquoted (typed via the result's column OIDs), NULL → `null`.
  v1 is the simple-query protocol; `?`-parameter binding (extended protocol) is the
  next milestone.
- **`read_bytes(fd) -> bytes`** — NUL-safe socket read. `read()` returns a C string
  and truncates at the first 0 byte; a binary wire protocol needs the raw bytes.
- **`base64_encode_bytes(bytes) -> string` / `base64_decode_bytes(string) -> bytes`**
  — binary-safe base64 (the string forms stop at a NUL), for SCRAM salts/proofs and
  any binary token.
- SCRAM's XOR folds (PBKDF2, ClientProof) use MFL's native bitwise operators
  (`^ & | << >>`) — no new builtin needed.

## v0.59.1

Agent-discovery polish — fixes surfaced by dogfooding the north-star flow ("learn
machin and build a users back-office" from a cold start). No language changes.

- **`machin guide` clarified** where a fresh agent silently shipped a bug:
  `json_get` now states it returns the **raw JSON token** (a string field comes
  back **quoted**); `parse` documents its **slice witness** (`parse(jsonArray,
  []T{}) -> []T`); `sqlite_query` points at `parse(rows, []T{})` as the
  row-iteration idiom; a new `sqlite-rows-decode` gotcha ties it together.
- **`skills/machin-web/SKILL.md`**: fixed the wrong `sqlite_query -> []string`
  signature (it returns a JSON-array string); led the CRUD recipe with
  `parse(rows, []User{})`; documented the form-encoded POST-body path (use the
  `url_decode` builtin + a `form_field` helper — don't hand-roll `url_decode`,
  it's a builtin and shadowing it is a compile error).
- **New runnable example** `examples/complex/sqlite_crud.mfl` — the SQLite data
  layer end to end (the repo had no in-tree SQLite example before).
- **`AGENTS.md`** dogfood section now points web-building agents at the
  `machin-web` skill (previously only game-dev was signposted).

## v0.59.0

- **Fix: a closure can capture and mutate an aggregate local** (a slice, map, or
  struct). The heap box for such a captured local is now zeroed via `calloc`;
  codegen previously emitted `*box = {0}`, invalid C for an aggregate type (it
  compiled only for scalars). So a closure that captures `xs := []int{}` and
  `append`s to it across calls now works.
- **`framework/reactive.src` rewritten on the simpler model** the captured-closure
  fixes (v0.58.0 + this) unlock: a reaction is now just a **`func()` thunk that
  captures its own compute closure** and applies its effect, in one `[]func`
  registry — instead of typed per-kind arrays (`c_fn`/`b_fn`/`e_kf`/…) with a
  `run_computed`/`run_bind`/`run_each` dispatch. `bind` captures its `last`, `each`
  its `old` key set, `computed` is a signal kept current by a reaction. Same public
  API, **byte-identical patch behavior** (verified), ~9 fewer globals, the dispatch
  gone. (Re-confirmed gotcha: a parameter named like a builtin — `keys` — is
  shadowed at call sites; `each`'s param was renamed `keyfn`.)
- **`framework/router.src` — a client-side router** (composes with reactive, enabled
  by the cleaner `reaction()` primitive). The active route is a signal (an int
  index): `route(path)` registers pages, `navigate(i)` / `nav(path)` switch,
  `link(path, label)` renders an anchor, and `outlet(id, render)` re-renders the
  active page (a reaction over a new `dom_html` host import) and syncs the address
  bar (`nav_url`). `nav` takes the path string from the host via `ptr_str`. For
  multi-page admin apps / SPAs.

## v0.58.0

- **Fix: a captured closure can be CALLED inside a lambda.** `func(){ fn() }` where
  `fn` is a captured variable now works — previously it failed to compile (`call to
  undefined function`), because closure conversion's free-variable scan ignored a
  call's *callee*, so the closure was never captured. This is the higher-order
  function building block: functions that return closures calling their arguments,
  and the clean reactive shape `effect(func(){ compute() })` (a stored `func()`
  thunk that calls a captured compute). Top-level functions/builtins are unaffected
  (they were never in the enclosing scope, so they're still not captured). One-line
  fix in `freeIdents`; no regressions.

## v0.57.0

- **`ptr_str(ptr) -> string` — host→wasm strings (text input / forms).** Reads a
  NUL-terminated string out of raw memory into an MFL (arena) string. This is the
  missing **into-wasm** string direction: the JS host writes UTF-8 + a NUL into the
  module's memory at a pointer the program `alloc`'d, then calls an export passing
  that pointer, and the program reads it with `ptr_str`. (Strings already flowed
  *out* of wasm as a memory pointer the host decodes; ints flow both ways.) Pairs
  with `alloc`/`free`. Verified round-trip incl. multi-byte UTF-8 (accents, emoji).
  Unblocks forms — an `<input>`'s text can now reach a machin component.

  ```go
  export func input_buf(n) (p) { p = alloc(n + 1) }     // host writes n bytes + NUL here
  export func submit(p) { add_todo(ptr_str(p))  free(p) }
  ```

## v0.56.0

- **`framework/reactive.src`: `hydrate` + a value-embedding `slot` — isomorphic
  apps.** A server can SSR-render a component's markup and the wasm client now
  attaches its reactivity to that **existing DOM** instead of re-rendering it:
  - **`slot(name, compute)`** now embeds the current value in its markup
    (`<span data-s="name">VALUE</span>`), so `mount` paints filled markup (no
    first-paint flash) and so SSR markup and client markup agree.
  - **`hydrate(html)`** flushes the queued bindings/lists **without** a `dom_mount`
    — it connects them to the SSR elements by their `data-s` / container names.
    `mount` is now `dom_mount` + `hydrate`.

  So one component renders on the server (first paint, works with JS off) and
  hydrates in the browser with no re-render. Drove
  [boilerplate-cli-ui-machin-isomorphic](https://github.com/javimosch/boilerplate-cli-ui-machin-isomorphic)
  — a single binary that is a CLI + HTTP server + JSON API + reactive wasm UI, the
  full-stack-MFL capstone.

## v0.55.0

- **`framework/reactive.src` gains a templating layer** — a component declares its
  markup *and* its reactive bindings in one place, instead of a hand-written HTML
  skeleton plus manual `data-s` wiring:
  - **`slot(name, compute)`** returns the markup for a reactive text node and
    queues its binding.
  - **`list(name, keys, item)`** returns the markup for a keyed-list container and
    queues its reconciler.
  - **`mount(root, html)`** sets the root element's HTML once (via a new
    `dom_mount` host import), then flushes the queued bindings/lists so they paint
    after their elements exist.

  So a whole component is one expression: `mount("app", "<h1>…</h1>" + slot("count",
  fn) + list("items", keys, item))`. Static markup is plain string concatenation;
  the JS host shrinks to a `dom_mount` + the patch ops. Pure MFL (no compiler
  change). Updated [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive)
  to generate its own markup — `index.html` is now just `<div id="app"></div>`.

## v0.54.0

- **`framework/reactive.src` grows computed signals + keyed list reconciliation.**
  - **`computed(func(){ return … })`** — a memoized derived signal (backed by a
    real signal, kept current by a reaction); read it with `get` and depend on it
    like any signal. Transitive: a change recomputes the computed, which updates
    its own dependents.
  - **`each(container, keys, item)`** — keyed list reconciliation. `keys()` returns
    the ordered keys as a CSV string (`func(){ get(ver)  return csv(ids) }`),
    `item(key)` returns an item's HTML. On a change it emits only the deltas —
    `list_insert` for new keys (rendered once), `list_remove` for gone keys, and a
    `list_order` directive — never re-rendering unchanged items. The unified
    reaction graph means a computed/binding/list all recompute only when a signal
    they read changes, and only changed text/keys patch.
- **Defining a function named like a builtin is now a compile error** (it would be
  silently shadowed by the builtin at call sites — a footgun that bit the reactive
  runtime three times: `flush`, `keys`, `contains`). Rename it. An `extern` may
  still shadow a builtin (intentional, for FFI). Drove
  [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive)
  (now a list + computed demo).

## v0.53.0

- **Slices of functions — `[]func`.** A slice literal may now have `func` as its
  element type (`fns := []func{}`), so closures can be stored, appended, indexed,
  and called from a slice — the dispatch-table / callback-list / effect-registry
  primitive. (Each element is an `mfl_closure`; the slice machinery already handled
  by-value structs, so this was a one-token parser fix.)
- **`framework/reactive.src` — a fine-grained reactive runtime for web UIs** (the
  Solid/Leptos model, in MFL). Built on `[]func` (a binding registry of compute
  closures) and package globals (the signal store): **signals** hold state
  (`signal`/`get`/`set`), **bindings** are compute closures tied to a DOM slot
  (`bind(slot, func(){...})`) whose signal reads are auto-tracked as dependencies,
  and on `set` only the bindings that read that signal recompute — emitting a
  **patch** (`dom_patch(slot, value)`) only when the rendered text actually
  changed. The host sets the text of the handful of changed slots; no innerHTML
  replacement, no vdom diff. Drove [machin-web-demo-reactive](https://github.com/javimosch/machin-web-demo-reactive).
  - Gotcha it surfaced: a user function named like a builtin (e.g. `flush`) is
    silently shadowed by the builtin — the runtime's internal `commit` was renamed
    to avoid it. Lambda **named returns** aren't supported yet (`func() (s) {…}`);
    use `func() { return … }`.

## v0.52.0

- **Package-level variables — `var name = expr` at top level.** A mutable global
  shared by every function, its type inferred from the initializer (and its uses).
  Unlike a local it **persists across calls**, so a wasm module's exported
  functions can finally hold state between host invocations — `var count = 0` +
  `export func bump(d) { count = count + d }` accumulates across calls. The piece
  that was missing for a component to own its state in machin (not the JS host).
  - `=` to a global's name assigns the global; `:=` still introduces (and may
    shadow with) a local. Globals are visible everywhere, including inside
    closures — a captured global is referenced directly, not closed over.
  - Works for any type: scalars, strings, **maps** (`var hits = make(map[string]int)`,
    then `hits[k] = …`), and **slices** (`var log = []string{}`, then `append`).
  - Implementation: each global is a C static `mfl_g_<name>`; the initializers run
    in a `constructor` — before `main` (native) and at `_initialize` (wasm reactor).
    `encode` keeps a brace-less top-level `var` as its own declaration.

## v0.51.0

- **Binary HTTP bodies — a machweb server can serve its own wasm (and any binary
  asset).** Two NUL-safe builtins over the `bytes` type:
  - **`read_file_bytes(path) -> bytes`** — read a whole file's raw bytes (the
    existing `read_file` returns a C string and truncates at the first NUL, so it
    can't carry a `.wasm`/image/font).
  - **`write_bytes(fd, bytes) -> int`** — write the exact bytes of a buffer to an
    fd (a full-write loop), unlike `write` which `strlen`s a string body.

  [`framework/machweb.src`](framework) gains a binary response path: the `Response`
  carries an optional `bin bytes` (+ `is_bin` flag), with builders **`ok_bytes(ctype,
  b)`** and **`ok_wasm(b)`**; `machweb_handle` writes the headers then the raw bytes
  when `is_bin` is set. So a single native machin binary can ship both its SSR HTML
  *and* its own `.wasm` SPA bundle — verified byte-identical over HTTP, and the
  served module still instantiates. The keystone for **full-stack MFL** (one binary,
  server-rendered then hydrated); drove
  [machin-web-demo-ssr](https://github.com/javimosch/machin-web-demo-ssr). `write_bytes`
  is gated like the rest of the socket runtime (native always; wasm only when used).

## v0.50.0

- **`--target wasm` — machin compiles to WebAssembly (frontend / in-browser).**
  `machin build app.mfl --target wasm` cross-compiles the emitted C to a
  `wasm32-wasi` **reactor** module via `zig cc` (zig bundles clang + a wasi-libc,
  so it is a single-binary C→wasm toolchain — no emscripten/wasi-sdk; override
  with `ZIG=`). The module loads in a bare browser; the bridge is machin's own
  **FFI, both ways**:
  - **`export func name(...)`** — a new keyword that marks a function as a wasm
    **export** (and a reachability root, kept even if `main` never calls it). The
    host calls `instance.exports.name(...)`; the export carries an `export_name`
    attribute so JS sees the clean source name, not the mangled C symbol. A wasm
    module needs **no `main`** — an export is its own entry point.
  - **`extern "env" { fn set_html(string) }`** — under the wasm target a headerless
    extern becomes a wasm **import** (`import_module`/`import_name`) the host (JS)
    supplies, e.g. a DOM call. The `extern "<lib>"` name is the import module
    (default `env`).
  - Marshaling: machin ints are i64 ⇒ `BigInt` across the boundary; strings cross
    as a pointer into the exported `memory` (decode NUL-terminated UTF-8 host-side).
- **The POSIX socket/tty runtime is now pay-as-you-go.** The networking
  (`listen`/`accept`/`dial`/`read`/`write`/`close`) and terminal
  (`raw_mode`/`read_key`) runtimes are split out of the always-on core. The
  **native** target is unchanged (it always carries them, and still emits `int
  main`); the **wasm** target emits each only when the program actually uses it,
  so a frontend module references no `socket()`/`termios` symbols (which wasi-libc
  doesn't fully provide) and compiles clean — the same `usesX` gating machin
  already does for TLS/WebSocket/regex/math/SQLite. Drove
  [machin-web-demo-wasm](https://github.com/javimosch/machin-web-demo-wasm); see
  `docs/NORTH-STAR-WEB.md`. Still ahead for a richer frontend: package-level
  globals (state lives host-side today) and a shipped JS string/array runtime.

## v0.49.0

- **`noise2` / `noise3` — native Perlin gradient noise.** Deterministic, ~`[-1,1]`,
  smooth/continuous; 2D and 3D. The backbone of procedural worlds — layer it (fbm,
  in MFL) for terrain, animate 2D noise over time with `noise3`. Pure C + libm's
  `floor`; the runtime is emitted and `-lm` linked only when used. Drove a full
  procedural planet ([machin-game-demo-cyberpunk](https://github.com/javimosch/machin-game-demo-cyberpunk)):
  infinite chunk-streamed terrain + procedurally placed buildings, all from noise.

## v0.48.0

- **Pointer-bearing `cstruct` fields + inout `T*` params.** Two follow-ups to the
  v0.47.0 pointer/array FFI that let MFL declare and pass C structs containing
  pointers, instead of poking raw bytes at hard-coded offsets:
  - a `cstruct` **field** may be **`ptr`** — held as an `int` in MFL, cast through
    `void*` at the boundary (C converts to `float*`, `unsigned char*`, …). So a
    struct like raylib's `Mesh` is declared with its pointer fields and the C
    compiler lays it out.
  - a new **inout** param form **`T*`** (`T` a declared `cstruct`) — the arg is a
    cstruct *variable*, marshaled to a C temporary, passed **by pointer**, and the
    modified struct **written back** afterward (e.g. `fn UploadMesh(Mesh*, bool)`
    returns the GPU vao/vbo ids in the mesh).

  Together these drop the hard-coded `Mesh` byte offsets from
  [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet): the GPU
  mesh is now a `cstruct Mesh { … vertices ptr colors ptr … }` built by value and
  uploaded via the inout `Mesh*`. Resolves the rough edge noted in v0.47.0.

## v0.47.0

- **Pointer/array FFI — raw memory + `*T` params.** machin can now build C
  buffers and structs and hand them to a foreign API:
  - raw heap memory (pointers are `int`s): `alloc(n)` (zeroed), `free(p)`,
    `poke_f32`/`poke_i32`/`poke_u8`/`poke_u16`/`poke_ptr(p, byteOffset, v)`,
    `peek_f32`/`peek_i32(p, byteOffset)`.
  - a new FFI param convention **`*T`** — the MFL arg is a pointer (`int`); the
    call dereferences it and passes the pointed-to C struct **by value** (e.g.
    `fn LoadModelFromMesh(*Mesh) Model`). Pass the pointer itself with `ptr`
    (`void*` → any `T*`), so an in/out `UploadMesh(Mesh*)` writes back into the
    buffer.
  - Also: an explicit `extern` declaration now resolves before the builtin
    switch (introduced in v0.46.0), so reaching a foreign `fn` of the same name
    as a builtin still works.

  This is the first FFI tier that hands C **raw pointers/arrays**, unlocking GPU
  vertex buffers (`UploadMesh`/`LoadModelFromMesh`/`DrawModel`) — a procedurally
  generated mesh built in MFL and uploaded to VRAM. Surfaced (and verified) by
  [machin-game-demo-planet](https://github.com/javimosch/machin-game-demo-planet). The
  Tier-2 unlock in the [game-dev north star](docs/NORTH-STAR-GAMEDEV.md).

## v0.46.1

- **docs:** fix the guide's `ffi-nested-cstruct` gotcha, which still said "no
  native sin/cos/sqrt yet" after v0.46.0 added them; point to the `math` builtins
  instead. Refresh the `machin-gamedev` skill's math note + dogfood record.

## v0.46.0

- **Native math builtins.** A floating-point math suite over libm:
  `sin cos tan asin acos atan atan2 sqrt cbrt pow exp log log2 log10 floor ceil
  round trunc abs fmod hypot` and `pi()`. Numeric in, `float` out; libm is linked
  (`-lm`) and the runtime emitted **only when a math builtin is used**, so
  math-free programs keep their libc-only footprint. An `extern` declaration of
  the same name still shadows the builtin (so existing `extern "m" { fn sqrt ... }`
  code is unchanged). Surfaced by [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d),
  which had to reach libm via `extern "m"` for its camera orbit — now `sin`/`cos`
  are native. The driver for procedural-animation apps.

## v0.45.0

- **FFI nested cstructs.** A `cstruct` field may now be another declared
  `cstruct`, not just a numeric scalar — a by-value struct of by-value structs.
  The synthesized MFL struct nests the inner `mfl_` type and the boundary
  marshaling recurses (`mfl_from_`/`mfl_to_` per field). This unblocks **3D**:
  raylib's `Camera3D` is three `Vector3`s + scalars, so it couldn't be expressed
  before; now `Camera3D{Vector3{...}, Vector3{...}, Vector3{...}, 45.0, 0}`
  constructs and passes by value to `BeginMode3D`. Surfaced building
  [machin-game-demo-3d](https://github.com/javimosch/machin-game-demo-3d). (Also unlocks 2D
  cameras and any struct-of-structs C API.) Note the orbit math there still goes
  through libm via `extern "m"` — machin has no native `sin`/`cos`/`sqrt` yet,
  the next gap.

## v0.44.0

- **FFI opaque handles — `cstruct Name {}`.** An empty-body `cstruct` declares a
  by-value C type (from the `header`) that machin holds and passes back **without
  naming its fields**. This is for by-value structs that contain pointers and so
  can't be a numeric `cstruct` — e.g. raylib's `Sound`/`Music`/`Font`. MFL can
  receive one from a `fn`, store it (a variable or `[]Name`), and pass it to
  another `fn`; it can't construct or field-access it. machin wraps the real C
  struct in one hidden field and copies it whole at the boundary, so the existing
  by-value marshaling path carries it. Surfaced building
  [machin-game-demo-simon](https://github.com/javimosch/machin-game-demo-simon), whose audio
  needs `LoadSound`→`PlaySound` over raylib's pointer-bearing `Sound`. Unlocks
  every "load a handle, pass it back" C library, not just audio.

## v0.43.0

- **`float()` — int → float conversion.** The counterpart to `int()`. MFL has no
  implicit `int`→`float`: only a flexible numeric *literal* promotes against a
  float; a *concrete* int (a function return, `byte_at`, `len`, a typed param, an
  `int`-slice element, or an `f32`/`f64` FFI struct field) was a hard
  `int vs float` mismatch. `float(x)` lifts it. Surfaced building the physics and
  random-pipe placement in [machin-game-demo-flappy](https://github.com/javimosch/machin-game-demo-flappy),
  where `byte_at`-derived randomness and pixel coordinates are all float.

## v0.42.0

- **`str` accepts `bool` and `string`.** `str(true)` → `"true"`, `str(false)`
  → `"false"`, and `str` of a string is the identity. `str` was numeric-only, so
  `"moved=" + str(moved)` was a `bool vs num` type error — a papercut everyone
  worked around with a hand-written `b2s` helper. Surfaced repeatedly building
  the game logic in machin-game-demo-snake / machin-game-demo-2048. Non-stringable kinds
  (slice, map, struct, …) are still a clean compile error.

## v0.41.0

- **Terminal input: `raw_mode(on)` + `read_key()`.** Real-time, per-keypress
  terminal input. `raw_mode(1)` puts the tty in cbreak/no-echo mode (and
  `raw_mode(0)` restores it); `read_key()` is a non-blocking single-key read —
  a 1-char string, or `""` if nothing is waiting. `input()` was line-buffered
  (it blocks for a whole line + Enter), which interactive TUIs and games can't
  use. Surfaced by [machin-game-demo-snake](https://github.com/javimosch/machin-game-demo-snake)
  — and it unlocks every future terminal UI (pickers, progress views, REPLs).

## v0.40.0

- **Bitwise operators + hex/binary/octal literals.** `& | ^ << >>` (and unary `^`,
  complement), `int`-only, with Go's precedence (`<< >> &` bind like `* / %`;
  `| ^` like `+ -`). Integer literals now accept `0xff`, `0b1010`, `0o17` (with
  `_` separators). The whole binary/crypto/protocol surface (machin-protobuf,
  machin-wabin, machin-signal, machin-noise) had been faking these with `* / %`
  over powers of two — `cb >> 4 & 0x0f` instead of `(cb / 16) % 16`. Surfaced by
  that accumulated real usage.

## v0.39.0

- **The HTTP client now does plain `http://`, not just TLS.** `http_get`,
  `https_get`/`https_post`, and `http_request` previously rejected `http://` with
  `err="scheme"`; now they connect over a plain TCP socket for `http://` URLs
  (default port 80) and TLS for `https://` (443), sharing the same
  request/redirect/chunked/Content-Length handling — so `http→https` redirects
  follow transparently. Surfaced building machin-watch (an uptime monitor wants
  to watch plain-HTTP endpoints).

## v0.38.0

- **`xeddsa_sign` / `xeddsa_verify` builtins — XEdDSA (Curve25519 signatures).**
  The signature scheme Signal/WhatsApp use for identity and device signatures
  (signing a Curve25519 key, not plain Ed25519). Backed by libsodium's Ed25519
  group/scalar ops + OpenSSL SHA-512 + TweetNaCl field arithmetic for the
  Montgomery→Edwards conversion; matches libsignal's `SignCurve25519.go`. Emitted
  and linked (`-lsodium -lcrypto`) only when used — **requires libsodium-dev** on
  the build host. Surfaced building WhatsApp device pairing (machin-wapair).

## v0.37.0

- **`bytes` is now a first-class declarable type.** It was inference-only (locals
  from `bytes()`/crypto builtins); now it's usable in `struct` fields, `map`
  values (`map[string]bytes`), and `[]bytes`, so you can hold binary state in a
  record — needed for protocol state machines (e.g. a Noise handshake). One-line
  type-checker change; the C type (`mfl_bytes`) already existed.

## v0.36.0

- **Binary WebSocket frames: `wss_send_bin(conn, bytes)` / `wss_recv_bin(conn) -> bytes`.**
  The existing `wss_send`/`wss_recv` are text (`char*`) and truncate at a NUL; the
  binary variants carry a `bytes` payload (send as opcode `0x2`, recv NUL-safe).
  The frame loop is refactored into a shared core, so text and binary recv behave
  identically (ping/pong, fragmentation, close). Step 3 of the native-WhatsApp path
  (the protocol is binary framing).

## v0.35.0

- **Crypto builtins over `bytes` (OpenSSL libcrypto).** Step 2 of the native-crypto
  path: `rand_bytes`, `sha256_bytes`, `hmac_sha256_bytes`, `hkdf_sha256`,
  `x25519_pub`/`x25519_shared`, `ed25519_pub`/`ed25519_sign`/`ed25519_verify`,
  `aes_gcm_encrypt`/`decrypt`, `aes_cbc_encrypt`/`decrypt` — thin wrappers over
  OpenSSL, all operating on `bytes`. Emitted and linked (`-lcrypto`) only when a
  program uses one, so crypto-free programs stay lean. This proves the viability
  checkpoint: machin can do an X25519 ECDH handshake natively. (Digests match
  OpenSSL byte-for-byte; X25519 agreement, Ed25519 sign/verify, and AES-GCM/CBC
  round-trips all verified.)

## v0.34.0

- **`bytes` type — a NUL-safe binary buffer.** machin strings are NUL-terminated
  `char*`, so they can't hold arbitrary binary (anything with a `0x00` byte gets
  truncated). The new `bytes` type (pointer + length) can. Builtins: `bytes(str)`,
  `bytes_str(b)`, `to_hex`/`from_hex`, `byte_at`, `bytes_sub`, `bytes_concat`;
  `len(b)` works, and `println(b)` prints hex. This is the foundation for binary
  protocols and real crypto (step 1 of a native WhatsApp client — see machin-meet).

## v0.33.0

- **`http_request(method, url, headers, body)` builtin.** Authenticated HTTPS for
  any method, with caller-supplied header lines — the piece `https_get`/`https_post`
  lacked (they hard-code the header set, so no `Authorization`). `headers` is a
  `[]string` of `"Key: Value"` lines; returns `(status, body, err)` like `http_get`.
  Surfaced wiring WhatsApp booking notifications into machin-meet (the WhatsApp
  Cloud API and Twilio both require a bearer/basic `Authorization` header).

## v0.32.0

- **`url_encode` / `url_decode` builtins.** Percent-encoding for URLs (RFC 3986):
  `url_encode` keeps the unreserved set `A-Za-z0-9-._~` and `%XX`-encodes the rest
  (space → `%20`); `url_decode` reverses it leniently (`+` → space, malformed `%XX`
  passes through). Surfaced building machin-qs (a query-string ⇄ JSON converter),
  and lets servers like machin-meet decode query/form values safely.

## v0.31.0

- **`time_format_utc(unix, fmt)` builtin.** Like `time_format` but in UTC
  (`gmtime` instead of `localtime`) — the form iCalendar `.ics` and RFC-3339
  timestamps want, without the `%z`-offset arithmetic dance. Surfaced finishing
  machin-meet, whose `.ics` `DTSTART`/`DTEND` must be in UTC.

## v0.30.0

- **`time_make(y, mo, d, h, mi, s)` builtin.** Build a Unix timestamp from local
  calendar fields — the inverse of `time_fields`, backed by `mktime(3)` (which
  also normalizes out-of-range fields, so day 32 rolls into the next month). This
  completes the time trio: construct ↔ decompose ↔ render. Surfaced building
  machin-meet (a one-person self-hostable Calendly), which needs "09:00 local on
  date X → which Unix second?" to enumerate bookable slots.

## v0.29.0

- **`time_format(unix, fmt)` builtin.** Format a Unix timestamp (local time) with
  a `strftime(3)` pattern — `%Y-%m-%d`, `%H:%M:%S`, weekday/month names (`%A`/`%B`),
  zone name/offset (`%Z`/`%z`), `%F`, `%T`, and the rest. The pieces `time_fields`
  can't give you (locale names, zone). Surfaced building machin-date (a `date(1)` clone).

## v0.28.0

- **`time_fields(unix)` builtin.** Decompose a Unix timestamp (local time) into
  `[year, month, day, hour, minute, second, weekday(0=Sun), yearday]` — the
  calendar view machin lacked (it had `now()` but no way to read its parts).
  Backed by `localtime_r`. Surfaced building a cron-expression evaluator.

## v0.27.0

- **Parameterized SQLite queries.** `sqlite_exec` and `sqlite_query` now take an
  optional third argument — a `[]string` whose values bind to the `?`
  placeholders, in order (via `sqlite3_bind_text`). This is **injection-safe**: a
  value containing SQL is stored/compared literally, never executed. The two-arg
  forms are unchanged.

## v0.26.0

- **SQLite builtins — `sqlite_open`, `sqlite_exec`, `sqlite_query`, `sqlite_close`.**
  Real database storage, backed by `libsqlite3`. `sqlite_open(path)` returns a
  handle (`:memory:` for in-memory); `sqlite_exec` runs result-less SQL;
  `sqlite_query` runs a SELECT and returns a **JSON array of row objects**
  (INTEGER/REAL unquoted, TEXT escaped, NULL null) — so it composes with
  `json_get`. Emitted and linked (`-lsqlite3`) only when a program calls
  `sqlite_*`. Surfaced building a persistent key-value store.

## v0.25.0

- **`read_stdin()` builtin.** Reads all of stdin verbatim until EOF — exact
  bytes, no line splitting — unlike the line-based `input()` (which strips
  newlines and loses the trailing-newline distinction). This is what lets a tool
  process its input byte-exact (an exact byte count, a precise webhook body, a
  binary-ish payload). Surfaced building a `wc` clone.

## v0.24.0

- **Hash builtins — `sha256`, `hmac_sha256`.** `sha256(s)` and
  `hmac_sha256(key, msg)` return a lowercase hex digest. Pure C (no dependency),
  byte-exact against `sha256sum`/`openssl` (FIPS-180-4 + RFC 2104 test vectors).
  The common use is verifying webhook signatures (GitHub `X-Hub-Signature-256`,
  Stripe). Surfaced building a webhook signature verifier; completes the
  decode-then-verify story machin-jwt started.

## v0.23.0

- **Base64 builtins — `base64_encode`, `base64_decode`.** `base64_encode` emits
  standard padded base64; `base64_decode` is lenient — it accepts the standard
  and url-safe alphabets (`-`/`_`) and ignores padding/whitespace, so it also
  decodes JWT segments. Pure C (no dependency), in the always-on runtime.
  Surfaced building a JWT decoder.

## v0.22.0

- **Regex builtins — `regex_match`, `regex_find`, `regex_groups`, `regex_replace`.**
  POSIX extended-regex (ERE) over the subject string: test a match, extract the
  first match, pull capture groups (`[0]` whole, `[1..]` subgroups), or replace
  all matches. Backed by libc's `<regex.h>`, emitted only when a program uses
  `regex_*` (so others stay portable). Surfaced building a grep.

## v0.21.0

- **Left-to-right evaluation order (fixes #142).** Operands and arguments now
  evaluate in source order, matching Go — previously machin inherited C's
  unspecified order, so `f() + g()` could run `g()` first. Codegen hoists
  side-effecting sub-expressions into sequenced temporaries (a GNU statement-
  expression) for binary ops, call arguments, slice/struct literals, and
  multi-return lists; pure expressions are untouched (no overhead). The
  `eval-order` note in `machin guide` now states the guarantee.

## v0.20.0

- **`machin guide` completeness pass.** A fresh-eyes audit confirmed the builtin
  (51) and keyword catalogs match the compiler exactly, and filled the gaps: new
  idioms for the *functions* surface (`variadic`, `named-returns`, `closure`,
  `generic`, `scoped-arena`) and new gotchas — struct **value semantics** (copied
  on pass; use a map for shared mutable state), **no map comma-ok** (`v, ok :=
  m[k]` doesn't compile), the `parse(s, T{})` **witness**, and **unspecified
  evaluation order** (the review found `f() + g()` runs right-to-left, unlike Go;
  tracked in #142). Now 14 idioms, 13 gotchas, all compiled by a test.
- **`framework/flags.src` — a CLI flag parser (MFL module).** Every machin tool
  hand-rolled its argument parsing; this is a reusable parser composed like
  `machweb` (`machin encode framework/flags.src yourtool.src`). Short/long flags,
  the `=` and space value forms, bool flags, defaults, positionals, typed getters,
  and an auto `--help`. Its value store uses maps (reference types) so updates
  survive the `Flags` struct being passed by value — no compiler change. Drove
  [machin-http](https://github.com/javimosch/machin-http) (get/post/head). Closes #138.

## v0.19.0

- **`machin guide` — self-describing feature catalog for agents.** One command
  emits machin's complete, version-exact surface — keywords, types, every builtin
  with its signature + one-line semantics (grouped by category), the core idioms
  as runnable snippets, and the gotchas — as **JSON by default** (`--text` for
  dense prose). It's generated from a single in-binary source-of-truth catalog,
  so an agent masters the language in one call and the reference can't drift from
  the implementation: a test asserts every catalogued builtin is recognized by
  the compiler, and that the catalog version matches the README badge.

## v0.18.0

- **`flush()` builtin.** Forces buffered stdout out (`fflush`). libc fully buffers
  stdout when it's a pipe, so a streaming program's output otherwise only appears
  when the buffer fills or the process exits; calling `flush()` after a write
  makes it visible immediately downstream. Surfaced by the streaming batcher,
  whose whole point is timely emission.

## v0.17.0

- **Comma-ok receive — `v, ok := <-ch`.** A receive now optionally reports
  whether the channel is still open: `ok` is `false` once it's closed and
  drained (and `v` is the zero value). Works standalone and as a `select` case
  (`case v, ok := <-ch:`). Relatedly, **`select` now treats a closed channel as
  ready** — its receive case fires (with `ok == false` if bound) instead of
  spinning — so a `select` loop can detect a source closing. Built on the
  existing `mfl_chan_recv2` plus a new `mfl_chan_tryrecv2`. Surfaced building a
  stream batcher that flushes on size, on a timer, or when the input ends.

## v0.16.0

- **Channels deep-copy slices and maps too.** v0.15.0 made channels safe for
  strings; now slices, maps, and structs containing them (nested arbitrarily)
  are deep-copied across the goroutine boundary as well, so a `chan []string`,
  `chan map[string]int`, or `chan SomeStruct{…[]T…}` value sent from a short-
  lived goroutine survives that goroutine's arena being reclaimed. Plain strings
  keep the fast offset-copy path; elements containing a slice or map round-trip
  through the per-type JSON serializer/parser (a general deep copy reused from
  `json`/`parse`). Scalars are still a plain memcpy.

## v0.15.0

- **Fix: strings sent over a channel survive the sender goroutine.** A channel
  copied only the element bytes — for a string, just the `char*` — so a string
  allocated inside a short-lived goroutine and sent over a channel dangled once
  that goroutine's arena was reclaimed (garbled/corrupt reads on the far side).
  Channels are now string-aware: `make(chan T)` records the byte offsets of every
  string reachable by value in `T` (a bare `string`, or a struct's string fields,
  recursing into nested structs); send **deep-copies** those strings into stable
  storage, and receive **adopts** them into the receiver's arena (freeing the
  intermediate — no leak). Scalars are unaffected; slice/map backings inside an
  element are still shared (documented). Surfaced by machin-pipe, which had to
  work around it by keeping inputs in main's arena.

## v0.14.0

- **Channel `close` + range-over-channel.** Channels could be made and used but
  never closed, so a consumer had no clean "no more data" signal — pools stopped
  via sentinel values and a stray `range`/receive blocked forever. `close(ch)`
  now marks a channel done (waking every blocked receiver); a receive drains the
  buffer then yields the zero value, and **`for v := range ch`** loops until the
  channel is closed and drained. `close` dispatches on its argument — a channel
  closes the channel, an fd still closes the fd. Built on a new `mfl_chan_recv2`
  (receive-with-ok) primitive. Surfaced building a streaming fetch pipeline whose
  stages terminate by closing their channels.

## v0.13.0

- **`select` — wait on multiple channels.** machin had goroutines and channels
  but no way to wait on more than one at a time, so timeouts, cancellation, and
  worker-pool collectors were impossible. `select { case v := <-ch: ... case ch
  <- x: ... default: ... }` takes the first ready case (receives tried before
  sends, in source order), runs `default` when nothing is ready, or blocks when
  there's no default. Implemented as a poll over the cases using a new
  non-blocking `mfl_chan_tryrecv` primitive; case bodies run outside the poll
  loop so `break`/`continue`/`return` affect the enclosing scope. Surfaced
  building a bounded worker pool that races results against a deadline.

## v0.12.0

- **JSON path queries — `json_get(json, path)`.** Every machin tool used to dig
  into JSON with fragile substring search. `json_get` walks a jq-style path
  (`.key`, `[index]`, chained — `.a.b[0].c`, `.` for the whole document) and
  returns `(value, err)`: `value` is the located value's raw JSON text, `err`
  is `""`/`"notfound"`/`"path"`/`"parse"`. It's a non-allocating scanner that
  respects nesting and string escapes (no tree built), and the second builtin to
  use the `value, err :=` convention. Surfaced building a `jq`-style query CLI.

## v0.11.0

- **Error handling reaches the builtins — `http_get` returns `(status, body, err)`.**
  machin's HTTP builtins collapsed every failure to `""`: a 404, a 503, an
  empty-but-OK body, and an unreachable host were indistinguishable, so a program
  couldn't *handle* errors. `http_get(url)` brings the Go-style `value, err :=`
  idiom to the builtin layer — `status, body, err := http_get(url)`, where a
  non-empty `err` is a transport failure (`"dns"`/`"connect"`/`"tls"`/`"scheme"`)
  and otherwise `status` is the real HTTP code. The multi-assign destructure path
  now recognizes multi-return builtins; the existing `https_get`/`https_post`
  (body-only) are unchanged, both now built on the same status-aware core.
  Surfaced building a link checker that has to classify why a URL is broken.
- **`exit(code)`.** Terminate the process with a status code — so a CLI can fail
  CI on a bad result (the link checker exits non-zero on a broken link).

## v0.10.0

- **Native WebSocket — `wss_open`, `wss_send`, `wss_recv`, `wss_close`.** A
  `wss://` client (RFC 6455) over real TLS, no subprocess. `wss_open(url)` does
  the HTTP/1.1 Upgrade handshake and returns a connection handle; `wss_send`
  masks and writes a text frame; `wss_recv` blocks for the next message,
  reassembling fragments and transparently answering pings and handling close;
  `wss_close` tears down. Built on a shared TLS core refactored out of the HTTPS
  client (one process-global `SSL_CTX`), emitted and linked (`-lssl -lcrypto`)
  only when used. Surfaced dogfooding a streaming scraper that had to shell out
  to `websocat` — this retires that crutch too: a Polymarket CLOB stream now runs
  fully native (`https_get` to resolve the token, `wss_*` to stream).

## v0.9.0

- **Native TLS — `https_get` and `https_post`.** machin's biggest networking
  gap is closed: an HTTPS client over real TLS (OpenSSL), no subprocess. `https_get(url)`
  and `https_post(url, jsonBody)` return the response body, handling cert
  verification (SNI + hostname), `Content-Length`, chunked transfer-encoding, and
  redirects. The OpenSSL runtime is emitted and linked (`-lssl -lcrypto`) **only
  when used**, so TLS-free programs keep their libc-only footprint. Surfaced
  building a Polymarket scraper that had to shell out to `curl`/`websocat` because
  machin couldn't open a TLS socket — this retires the `curl` crutch for REST.

## v0.8.0

More dogfooding: building a streaming WebSocket scraper drove these in.

- **`break` and `continue`.** Loop control was missing entirely — the only way
  out of a `for`/`while` was a flag variable. `break` exits the innermost loop,
  `continue` skips to its next iteration; both work in `for cond`, `for {}`, and
  `range` loops (range increments live in the C `for` clause, so `continue` is
  safe). Surfaced writing hand-rolled JSON/stream parsers in MFL.
- **`encode` — string- and comment-aware function splitting.** `splitFunctions`
  counted every `{`/`}` to find declaration boundaries, including braces inside
  string literals and `//` comments. Any function emitting JSON (`"{...}"`) or
  searching for a brace (`index(s, "}")`) failed with `unbalanced braces`. It now
  tracks string state and stops at `//`.

## v0.7.0

Dogfooding: real tools drove these in. A health checker added networking +
timing + parsing; a static-site generator added file I/O and caught a parser
bug. See [awesome-machin](https://github.com/javimosch/awesome-machin).

- **Outbound networking — `dial(host, port)`.** Connect a TCP socket to a remote
  host (DNS-resolved via `getaddrinfo`), returning an fd used with the existing
  `read`/`write`/`close`. machin was server-only (`listen`/`accept`); `dial` makes
  it a client too — HTTP clients, health checkers, anything that reaches out.
  Surfaced and filled while building a real tool (the "build real things" goal).
- **`now_ms()` and `parse_int()`.** Wall-clock milliseconds (for measuring
  latency) and string→int parsing (`0` on non-numeric). Both surfaced building
  the same tool — a concurrent HTTP health checker.
- **File I/O — `read_file`, `write_file`, `list_dir`, `mkdir`.** Read/write whole
  files, list a directory (excludes `.`/`..`), make a directory. Native builtins
  (no FFI), surfaced building a static-site generator.
- **Parser fix — string literals equal to a structural token.** A string like
  `")"` was mistaken for the closing delimiter, so `index(s, ")")` failed to
  parse; value-list loops are now punctuation-aware. Caught by the SSG.
- **CLI builtins — `args()`, `env()`, `now()`.** `args()` returns the
  command-line arguments (`[]string`; `args()[0]` is the program path) — the
  generated `main` now takes `argc`/`argv`. `env(name)` reads an environment
  variable (`""` if unset). `now()` returns Unix seconds. Together these let MFL
  programs be real CLIs (subcommands, flags, `$PORT`, uptime) — the basis for a
  machin-based CLI/server boilerplate.

## v0.6.0

- **C FFI (Phases 1–3).** An `extern "lib" { header "..." link "..." cflags "..."
  cstruct T { f ctype ... } fn name(types) ret }` declaration names foreign C
  functions; calls compile to direct C calls and `header`/`link`/`cflags` are
  threaded into `cc`. **Phase 1:** scalar types — `int`/`float`/`bool`/`string`
  plus sized `i8…u64`/`f32`/`f64` (sizes matter for ABI: raylib takes 32-bit
  `int`/`float`). **Phase 2:** `cstruct` declares a C struct's layout; machin
  synthesizes a matching MFL struct and marshals it by value across the boundary
  (pass and return). **Phase 3:** the `ptr` type — an opaque C handle (`void*`,
  e.g. `FILE*` or a window/texture handle) held as an MFL `int` and passed back
  to C, never dereferenced. New `examples/complex/ffi_math.mfl`, `ffi_struct.mfl`,
  and `ffi_ptr.mfl`; the path to the C ecosystem and a native GUI.
- **Native GUI demo — `examples/gui/game_menu.mfl`.** A clickable Start / Settings
  / Exit menu drawn with [raylib](https://www.raylib.com) through the FFI: opens a
  real OpenGL window, draws rectangles/text with a `Color` cstruct, and polls the
  mouse each frame — proving Phases 1–2 are enough to drive a real graphics
  library. `extern` blocks may now have multiple `link` directives, kept in order
  (`-lraylib -lGL -lm -lpthread -ldl -lrt -lX11`). A GUI binary links the system
  graphics stack and needs a display — not a no-deps binary, as with any native GUI.
- **Tightened canonical form (token-minimization).** The canonical `.mfl` now
  drops whitespace adjacent to operators/punctuation (`fib(n - 1)` →
  `fib(n-1)`), keeping only the spaces the lexer needs between word tokens. Zero
  semantic change; ~13% fewer agent tokens to write/edit the corpus, measured
  with the new `tools/tokmin.py`. The same harness showed the *intuitive*
  minimizations are dead ends — `func`→`fn` saves **0** tokens (both are single
  tokens already) and `println`→`pln` is *worse* (abbreviations fragment) — so
  whitespace is where the win is.

## v0.5.0

- **Plain text is the source of truth.** The `.mfl` form is now canonical plain
  text — one normalized function per line — instead of base64. The reason is the
  language's own north star: measured with `tools/tokcost.py`, base64 costs an
  agent ~2.5× the output tokens to write/edit (and ~9× for a one-character edit),
  taxing the very machine-speed it was meant to signal. Text is greppable,
  diffable, and editable in place. `machin run` still reads the base64 form, now
  produced on demand by **`machin pack`** for distribution. Machine-first now
  means *shaped for machine authoring* (terse, inferred, canonical,
  function-addressable), not *encoded*.
- **`input()` builtin** — read a line from stdin (`() -> string`), enabling
  interactive / native desktop CLI programs. New `examples/complex/game_menu.mfl`.
- **`tools/tokcost.py`** — a tiktoken harness that measures the agent write/edit
  token cost of a source form; the instrument behind the plain-text decision.

## v0.4.2

- **Windows binaries.** Releases now also ship `machin-<tag>-windows-amd64.exe`,
  alongside linux/macOS × amd64/arm64. Five prebuilt binaries per release.

## v0.4.1

- **Release automation.** Pushing a `v*` tag now cross-compiles machin for
  linux/macOS × amd64/arm64 (pure Go, static, ~2 MB) and attaches the binaries
  plus `SHA256SUMS.txt` to the GitHub release — no manual upload step.

## v0.4.0

Native-language depth: safety, real closures, and bounded memory — plus the
platform layer (framework, router, `func` type) that landed since v0.3.0.

- **`--safe` build mode.** `machin run|build <file> --safe` inserts runtime
  checks: a slice index out of range, integer division/modulo by zero, or
  integer `+`/`-`/`*` overflow prints a Go-style `panic:` to stderr and exits
  non-zero. Opt-in — the default build keeps zero check overhead.
- **By-reference closure capture.** Closures now capture enclosing variables by
  reference (Go semantics): a captured variable lives in a shared cell, so a
  closure can mutate state that outlives the call that made it. The
  counter/accumulator idiom (`func counter() { n := 0  return func() { n = n + 1
  return n } }`) works, and sibling closures share one cell.
- **Scoped arenas (`arena { }`).** Wrapping a loop body in `arena { ... }`
  reclaims everything allocated inside the block when it ends, keeping a
  long-lived loop's memory flat (measured ~240 MB → ~1.4 MB over a 1M-iteration
  allocating loop). Blocks nest and compose with goroutines and `--safe`.

- **machweb — a web framework written in MFL.** `Request`/`Response` types,
  response builders (`ok_text`/`ok_html`/`ok_json`/`created`/`bad_request`/
  `not_found`), `parse_request`, a `param(path, prefix)` path helper, and
  `serve(port, handler)` which dispatches each request — in its own goroutine —
  to a handler closure `func(Request) Response`. A backend compiles to a single
  native binary with no runtime dependencies. See [`framework/`](framework/).
- **Map-based router.** `new_router()` → `route(r, method, path, handler)` →
  `serve_router(port, r)`. Handlers live in a `map[string]func` keyed by
  `"METHOD PATH"`; routing is method-aware and unmatched requests return `404`.
- **The `func` type.** A function-value type whose signature is inferred by
  unification — it lets closures be stored in slices, maps
  (`make(map[string]func)`), and struct fields. This is what makes a router's
  handler table possible.
- **Multi-file `machin encode`.** `encode` now accepts several source files and
  concatenates them, so a framework and an app compose into one program:
  `machin encode framework/machweb.src myapp.src > app.mfl`.

## v0.3.0

Ergonomics, toward feeling like Go to write:

- **Named return values.** `func divmod(a, b) (q, r) { q = a/b; r = a%b; return }`
  — the named returns are zero-initialized locals; a bare `return` (or falling
  off the end) yields them.
- **Variadic parameters.** A function's last parameter may be variadic
  (`func sum(nums...)`), collecting trailing call arguments into a slice. Call
  with extra args (`sum(1, 2, 3)`) or spread a slice (`sum(xs...)`). Variadics
  are generic — one source function specialized per element type.

## v0.2.1

- **Arena memory management.** Value buffers (strings, slice backings, closure
  environments) are allocated from a per-goroutine arena and reclaimed in bulk
  when the goroutine returns; the main goroutine's arena lives for the whole
  program. This bounds the memory of a long-running concurrent server — under a
  12,000-request load the self-host server's RSS plateaus at ~1.8 MB instead of
  growing unbounded. (Subsystems that free explicitly — channels, maps — keep
  raw allocation.)

## v0.2.0

A consolidation release. MFL grew from a base64 POC interpreter into a
native-compiling backend language with the complete Go-flavored core, plus a
formal specification ([`SPEC.md`](SPEC.md)).

### Language

- **Compilation to native code** — programs are translated to C99 and compiled
  with `cc -O2`; values are unboxed. `fib(40)` runs in ~0.20s, on par with
  hand-written C. (The original tree-walking interpreter was removed.)
- **Static typing by inference** — no annotations; type clashes are compile errors.
- **Composite types** — slices `[]T`, structs (`type T struct { ... }`), and
  maps `map[K]V` (int/string keys), all unboxed.
- **Control flow** — `for cond {}`, `for {}`, `while`, and `for k, v := range x`
  over slices, maps, and strings.
- **Multiple return values** — `return a, b`, destructuring `q, r := f()`,
  parallel assignment, and the comma-ok pattern.
- **Closures & first-class functions** — `func(x){...}` literals with by-value
  capture, higher-order functions (lambda-lifting + closure conversion).
- **Generics** — functions are implicitly generic, specialized per concrete
  call-site type by monomorphization (no boxing, no annotations).
- **Concurrency** — `go` goroutines (pthreads), channels (`make(chan T)`,
  `<-`), and `sleep`.
- **Networking & JSON** — BSD sockets (`listen`/`accept`/`read`/`write`/`close`),
  bidirectional JSON (`json(x)` serialize, `parse(s, T{})` parse), and string
  operations — enough to write a concurrent JSON-over-HTTP API with routing.

### Tooling

- `machin run` / `build` / `build --emit-c` / `encode`.
- `Makefile`, MIT `LICENSE`, `SPEC.md`, and 35 runnable examples.
- 51 Go tests exercising the full surface via the native path.

## v0.1.0

Initial POC: MFL as base64 (one function per line), a tree-walking interpreter,
`run`/`encode`/`decode`, and a first set of examples.
