# Contributing to machin

The one thing that trips up new contributors: `.mfl` files aren't meant to be
typed by hand. Here's the actual workflow for reading and editing one.

## The `.mfl` format

A `.mfl` file is the canonical MFL source: **plain text**, one normalized
function (or type) per line, a blank line between declarations. It's already
human-readable — open it in an editor like any other source file; that
tightened one-liner *is* the source of truth. (`machin pack` can additionally
emit a dense base64 "packed" form for distribution/wire transfer, and `machin
run` accepts either form, but the form committed to this repo is always plain
text.)

Because each line is whitespace-tightened by the compiler, don't hand-edit a
`.mfl` line directly — use the authoring loop below so the result stays
canonical and typechecked.

## Authoring loop

1. Write your function(s) as loose, human-formatted Go-like text in a `.src`
   file — whitespace, comments, multiple lines, whatever's comfortable.
2. Run it through `encode`, which normalizes each function to one canonical
   line and **typechecks the result before printing anything**:

   ```bash
   go run . encode app.src > app.mfl
   ```

   If `encode` fails, fix `app.src` — nothing is emitted on error, so a
   `.mfl` file that exists in this repo is always known-good.
3. Multiple `.src` files concatenate in order, so a framework module can be
   composed ahead of an app:

   ```bash
   go run . encode framework/machweb.src app.src > app.mfl
   ```
4. Run or build the result:

   ```bash
   go run . run app.mfl
   go run . build app.mfl -o app
   ```

## Adding a new example

- Basic language features go in `examples/basic/`; larger/composite programs
  go in `examples/complex/`.
- Author it as a `.src` file, `encode` it into the matching `.mfl`, and commit
  the `.mfl` (the `.src` is scratch and doesn't need to be committed).
- `examples/run.sh` (and `make examples`) picks up every `*.mfl` under
  `examples/` automatically — no registration step needed.
- Name long-running programs so they match `*server*` or `*_api*` (e.g.
  `http_server.mfl`, `json_api.mfl`) — `examples/run.sh` skips those instead
  of hanging on a process that never exits.
- If the example demonstrates a real language feature, add a row to
  `examples/README.md` and a note in `docs/LANGUAGE.md`/`SPEC.md`.

## Build and test

```bash
make build   # go build -o bin/machin .
make test    # go test ./...
```

Tests must pass before a PR. `make examples` (or `./examples/run.sh`) is a
good sanity check that nothing regressed at runtime.

See [AGENTS.md](AGENTS.md) for the fuller dev workflow and standing
constraints (in particular: the `.mfl` source of truth is canonical plain
text, not base64).
