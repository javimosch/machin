# AGENTS.md

Orientation for agents working on **machin** (the toolchain) / **MFL** (the
language). Humans state intent; the machine reads and writes the code.

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
  don't guess. (Lesson already paid for: tokens are saved by removing what the
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
and a note in `SPEC.md` + `README.md`.

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

1. Update the version: bump the badge in `README.md` and add a section to the
   top of `CHANGELOG.md` (newest first). Commit to `main`.
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
  a C compiler on PATH. Say so in release notes.
- Versioning: minor bump (`v0.X.0`) for new language/library features, patch
  (`v0.X.Y`) for fixes and tooling/CI.
