# AGENTS.md

Orientation for agents working on **machin** (the toolchain) / **MFL** (the
language). Humans state intent; the machine reads and writes the code.

> **Start here: run `machin guide`.** It leads with a DOMAINS map (web / gamedev / backend) and embeds the domain how-tos — `machin guide --skill web` or `--skill gamedev` prints the full skill offline, so you never need to reverse-engineer a demo repo to learn a domain.
>
> **Learn the language:** It emits the
> complete, version-exact feature surface — keywords, every builtin with its
> signature, idioms, and gotchas — as JSON (default) or `--text` (prose), from
> the compiler's own source-of-truth catalog (so it never drifts). This file is
> about *contributing to the toolchain*; `machin guide` is about *writing MFL*.

> **First, make sure your `machin` is current.** The guide catalog is compiled
> *into* the binary, so a stale binary advertises a stale feature set — an agent
> running an old `machin` won't see recently added builtins and may wrongly
> conclude a capability is missing (this is what produced #196: `http_get`/`parse`
> already existed, but an out-of-date binary didn't surface them). `make install`
> copies the build to `$PREFIX/bin`, but nothing rebuilds it after a `git pull`,
> so the `machin` on your `PATH` can silently lag the source. Before relying on
> the language surface, rebuild and verify the versions agree:
>
> ```sh
> make install      # or: go build -o "$(command -v machin)" .
> test "$(machin guide | sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' | head -1)" \
>      = "$(sed -n 's/.*machinVersion = "\([^"]*\)".*/\1/p' guide.go)" \
>      && echo "machin is current" || echo "STALE: rebuild/install machin"
> ```
>
> (Also delete any stray `./machin` build artifact in the repo — it is gitignored
> and easy to run by accident.)

## Current direction: dogfood

The POC goal is met; machin is now grown by **building real things** and letting
real use surface the gaps, which then get filled in the language. Recent
examples: a concurrent HTTP health checker added `dial`/`now_ms`/`parse_int`; a
static-site generator added native file I/O (`read_file`/`write_file`/`list_dir`/
`mkdir`) and a parser fix. **When you ship a real app/tool built with machin,
add it to [awesome-machin](https://github.com/javimosch/awesome-machin)** (the
curated ecosystem list). Each app is its own public repo under `javimosch` with
a `build.sh` (`machin encode src/*.src > app.mfl && machin build app.mfl`).

> **When you discover an MFL gotcha, a new pattern, or an FFI caveat during
> dogfooding, record it in [machin-learn](https://github.com/javimosch/machin-learn)**
> — the agent knowledge base for MFL, seven skill files under `.agents/skills/`
> covering language, builtins, gotchas, FFI, terminal/TUI, networking, and
> storage/SQLite. Each skill is a self-contained `.md` an agent loads in one
> `read` call. Keeping machin-learn current means the next agent won't hit the
> same wall twice. (If the learning is specific to a domain — web, game-dev,
> backend — it also belongs in `skills/machin-*/SKILL.md` in this repo; the
> general MFL gotchas go in machin-learn.)

Building a **web app** (HTTP server, JSON API, SSR, a reactive wasm UI, or a
CRUD back-office)? Read [`skills/machin-web/SKILL.md`](skills/machin-web/SKILL.md)
first — the machweb/reactive/router frameworks, the wasm bridge + host↔wasm
marshaling, the SQLite data layer (`parse(rows, []T{})` to decode rows), build-and-
verify, and the gotchas. A runnable data-layer reference is
[`examples/complex/sqlite_crud.mfl`](examples/complex/sqlite_crud.mfl); direction
and gap roadmap live in [`docs/NORTH-STAR-WEB.md`](docs/NORTH-STAR-WEB.md).

Building an **SME backend** (a datastore client, auth, a service integration)? The
direction and capability/gap matrix live in
[`docs/NORTH-STAR-BACKEND.md`](docs/NORTH-STAR-BACKEND.md). machin already has embedded
SQLite and a pure-MFL **PostgreSQL** client ([`framework/postgres.src`](framework/postgres.src)
— wire protocol + SCRAM-SHA-256, no libpq); both return rows as JSON that
`parse(rows, []T{})` decodes. Writing a CLI tool? [`framework/flags.src`](framework/flags.src)
is a reusable flag parser (short/long flags, `=`/space values, bool flags,
defaults, positionals, auto `--help`) composed the same way as `machweb`
(`machin encode framework/flags.src yourtool.src`) — see
[`framework/README.md`](framework/README.md#also-here-flagssrc--a-cli-flag-parser)
for the full API.

Building a **game** (terminal TUI or raylib GUI/audio)? Read
[`skills/machin-gamedev/SKILL.md`](skills/machin-gamedev/SKILL.md) first — the
canonical setup, build-and-verify workflow, raylib FFI surface, and accumulated
caveats/gotchas (especially MFL's no-implicit-`int`→`float` rule), distilled from
the game/demo series. Game-dev has become a primary dogfood domain; its direction
and gap roadmap live in [`docs/NORTH-STAR-GAMEDEV.md`](docs/NORTH-STAR-GAMEDEV.md).

## What this is

machin compiles MFL to native code through C:

```
.mfl (canonical text) ──▶ parse ──▶ infer types ──▶ emit C ──▶ cc -O2 ──▶ native binary
```

| Stage | File |
|-------|------|
| Lex / parse | `lexer.go`, `parser.go` |
| Lambda-lift / closure-convert | `transform.go` |
| Type inference + monomorphization | `types.go` |
| C codegen | `codegen.go` |
| Build / run (invokes `cc`) | `build.go` |
| CLI | `main.go` |

The full language reference is [`SPEC.md`](SPEC.md).

## Standing constraints (do not violate)

- **The `.mfl` source of truth is canonical plain text.** One normalized
  function (or type) per line, a blank line between declarations. It is
  greppable, diffable, and editable in place — keep it that way. Machine-first
  means the language is *shaped for machine authoring* (terse, no type
  annotations, canonical one-line form, function-addressable), not that it is
  encoded. A dense base64 "packed" form exists via `machin pack` for
  distribution only; `machin run` reads either form, but the committed source is
  text. (This replaced a base64-as-source design: measured with `tools/tokcost.py`,
  base64 costs an agent ~2.5× the output tokens to write/edit — it taxed the very
  machine-speed it was meant to signal. See PRs/issue history.)
- **Machine-first / minimalism.** Prefer the smallest change that holds the
  surface minimal. The north star is *low agent write/edit cost* (output tokens)
  — measure form/syntax changes with `tools/tokcost.py` and `tools/tokmin.py`,
  don't guess. Before claiming a syntax change is reliability-neutral, run it
  through `tools/reliability/` too — tokens saved mean nothing if the change
  makes a model write the code wrong more often; real cost is tokens × tries.
  (Lesson already paid for: tokens are saved by removing what the
  tokenizer *charges* for — whitespace, ~13% — not by shortening keywords it
  already packs into one token; `func`→`fn` saves 0, `println`→`pln` is worse.)
  The canonical `.mfl` form is whitespace-tightened; keep emitted/committed code
  in it (`tighten` in `main.go`, guarded by `TestExamplesAreCanonical`). Target
  C/Rust/Zig-class performance — the default build has no runtime overhead a C
  programmer wouldn't accept.
- Keep the working tree clean. Commit/push only the intended change.

## Dev workflow

```bash
go build ./...                 # build the compiler
go vet ./... && go test ./...  # must be green before a PR
make examples                  # run every example (also: examples/run.sh)
```

Author MFL by writing loose Go-like `.src` text and minting `.mfl` from it:

```bash
go run . encode my.src > my.mfl      # multiple files concatenate (framework + app)
go run . run my.mfl                  # compile to native + execute
go run . run my.mfl --safe           # + bounds / div-zero / overflow checks
```

When adding a language feature, thread it through **every** pass that switches on
node type: `transform.go` (lifter, `collectDeclared`, `freeIdents`), `types.go`
(`genStmt`, and arity/return walkers), and `codegen.go`. Add a test in
`*_test.go` and, when it is user-facing, an example under `examples/complex/`
and a note in `SPEC.md`, `README.md`, and `docs/LANGUAGE.md`, plus the relevant
idiom/gotcha in the `machin guide` catalog (`guide.go`).

### Branch → PR → merge

Feature work goes on a branch, then a PR, then squash-merge + delete the branch.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- PR bodies end with:
  `🤖 Generated with [Claude Code](https://claude.com/claude-code)`
- Do not merge without the user's go-ahead.
- Stacked PRs: GitHub *closes* (does not retarget) a PR when its base branch is
  deleted on merge. If you stack, expect to rebase the child onto `main`
  (`git rebase origin/main` — already-merged commits drop out as duplicate
  patches) and open a fresh PR against `main`.

## Releasing

Releases are automated by [`.github/workflows/release.yml`](.github/workflows/release.yml).
Pushing a `v*` tag cross-compiles machin and attaches the binaries to that tag's
GitHub release. **To cut a release:**

1. Update the version: bump the badge in `README.md`, the `Version` line in
   `SPEC.md`, **`machinVersion` in `guide.go`** (a test enforces it matches the
   README badge), and add a section to the top of `CHANGELOG.md` (newest first).
   Also bump the other version-stamped, user-facing surfaces, which have **no
   automated guard** (the `guide.go` test only checks the README↔guide pair) and
   must be updated by hand: the landing-page badge in `docs/index.html`, the
   `version:` field in `selfhost/server.mfl`, and the monthly changelog pages
   (`docs/changelog-YYYY-MM.html` / `docs/changelog-YYYY-MM-product.md` — add
   the new release to the latest month's page). Commit to `main`.
   ```
   release: vX.Y.Z (one-line summary)
   ```
2. Tag and push:
   ```bash
   git tag -a vX.Y.Z -m "MFL vX.Y.Z" && git push origin vX.Y.Z
   ```
3. The workflow builds `machin-vX.Y.Z-<os>-<arch>` for **linux/{amd64,arm64}**,
   **darwin/{amd64,arm64}**, and **windows/amd64** (`.exe`), plus
   `SHA256SUMS.txt`, and publishes them. Watch it:
   ```bash
   gh run list --workflow=release.yml --limit 1
   gh run watch <run-id> --exit-status
   ```
4. Verify the assets landed:
   ```bash
   gh release view vX.Y.Z --json assets -q '[.assets[].name]'
   ```

Notes:
- The workflow is **idempotent for a tag**: if a release already exists for the
  tag (e.g. hand-authored), it overwrites assets (`--clobber`); otherwise it
  creates the release with `--generate-notes`. To re-run a build for an existing
  tag, delete and re-push the tag.
- machin is **pure Go** (no cgo), so binaries are static (~2 MB) and
  cross-compile cleanly. The shipped binary is a **compiler frontend** — it
  emits C and invokes `cc`/`gcc`/`clang` at build time, so end users still need
  a C compiler on PATH. The compiler frontend defaults to `cc`; set `CC` to
  override it, e.g. `CC=clang machin build app.mfl -o app`. Say so in release notes.
- Versioning: minor bump (`v0.X.0`) for new language/library features, patch
  (`v0.X.Y`) for fixes and tooling/CI.
