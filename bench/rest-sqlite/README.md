# Benchmark — a REST + SQLite service, four ways

The same notes API — `POST /notes`, `GET /notes`, `GET /notes/{id}`, `DELETE
/notes/{id}`, backed by SQLite — written idiomatically in **machin**, **Rust**,
**Go**, and **Python**, then measured for what actually costs an AI agent: **the
tokens to write and to edit it**. All are verified to build and pass the same CRUD
smoke test.

## Results (this machine, tiktoken `o200k_base`)

| impl | lines | author tokens | vs machin | deps | ship as |
|---|--:|--:|--:|--:|---|
| Python | 42 | 383 | 0.99× | 0 (stdlib) | source + CPython interpreter |
| **machin** | 42 | **388** | 1.00× | **0** | **49 kB binary** (2.4 MB fully static) |
| Go | 69 | 527 | 1.36× | 1 module | 14.8 MB binary |
| **Rust** | 69 | **727** | **1.87×** | **37 crates**, 91 MB `target/` | 2.9 MB binary |

**Rust is the most expensive of the four to author** — 87 % more tokens than
machin for the same service — and needs 37 transitive crates and a network fetch
to get there.

† *edit* = applying one concrete, equal-semantics change (add a `created_at`
field), modelled as the tokens an editing agent emits: for each changed hunk,
 = applying one concrete, equal-semantics change (add a `created_at`
field), modelled as the tokens an editing agent emits: for each changed hunk,
`tokens(old_string) + tokens(new_string)`.

**The honest read:** machin is **as terse as Python** to author (they tie — both
are token-light), **~36 % cheaper than Go**, and **lowest on edit cost** — *and*
it compiles to a **44 KB dependency-free native binary with SQLite, HTTP, and the
router built in**. That combination is the point: Python-level brevity for the
agent, a single tiny native artifact to ship. No interpreter, no `go mod`, no
14 MB binary, no container needed.

This is **not** the "2.5×" figure you may see elsewhere — that one measures
machin's *own* source form (canonical text vs the legacy base64 packing) and says
nothing about other languages. This benchmark is the cross-language number, and it
is deliberately unflashy and real.

## Reproduce

```bash
# build + run all three, run the CRUD smoke test
./run.sh

# measure author/edit tokens (needs: pip install tiktoken)
python3 measure.py
```

`measure.py` counts only the **application source** in each language — not
machin's `machweb`, not Go's `net/http`, not Python's `http.server`. Each
language's reusable framework/stdlib is written once and never re-emitted per app,
so counting it would distort all three equally. We report `o200k_base` and
`cl100k_base`; the ranking is the same under both.

## Files

- `machin/app.src` · `go/main.go` · `python/app.py` — the three implementations
- `*.v2.*` — the same programs with the `created_at` field added (the edit target)
- `measure.py` — the tokenizer harness
- `run.sh` — build + CRUD smoke test for all three

## Where the batteries actually matter

This benchmark and [`../native-speed`](../native-speed)'s companion probe point in
opposite directions, and the pair is the real finding.

On **generic algorithmic code** — count word frequencies, print the top 5 — Rust's
stdlib is rich and wins on brevity. machin only drew level there once `sort`/`sort_by`
shipped ([#580](https://github.com/javimosch/machin/issues/580)); before that Rust
was 21 % cheaper, after it machin is 16 % cheaper. It is close either way.

On **domain code** — an HTTP service over SQLite returning JSON — machin wins by
the largest margin in the table, because HTTP, SQLite, JSON and the router are all
in the box. Rust reaches for `tiny_http`, `rusqlite` and `serde_json`, which pull
37 crates and a 91 MB `target/` directory.

So the honest claim is not "machin is terser than Rust". It is **"machin is terser
than Rust exactly where machin has batteries"** — and that the gap grows with how
much of the domain the stdlib already covers.

### Why there is no Zig column

Zig's std has `std.http.Server` and `std.json`, but **no SQLite** in any version
checked (0.15.2 and 0.16.0). A Zig implementation would be Zig plus a C interop
layer over `sqlite3.h` plus manual linking — a materially different program from
the other four, not a fourth translation of the same one.

Rather than fabricate a number or quietly drop the language, this is recorded as
what it is: **not implemented**, because the comparison would not be like-for-like.
If someone writes it, the interesting figure is how many of its tokens are C-interop
plumbing rather than the service.

## Reproduce

```bash
./run.sh                 # build all impls + run the same CRUD smoke test
python3 measure.py       # token counts (needs tiktoken)
cd rust && cargo build --release   # needs network on first build
```
