# Benchmark — a REST + SQLite service, three ways

The same notes API — `POST /notes`, `GET /notes`, `GET /notes/{id}`, `DELETE
/notes/{id}`, backed by SQLite — written idiomatically in **machin**, **Go**, and
**Python**, then measured for what actually costs an AI agent: **the tokens to
write and to edit it**. All three are verified to build and pass the same CRUD
smoke test.

## Results (this machine, tiktoken `o200k_base`)

| impl | lines | author tokens | vs machin | edit tokens† | vs machin | ship as |
|---|--:|--:|--:|--:|--:|---|
| **machin** | 42 | **388** | 1.00× | **290** | 1.00× | **44 KB static binary, 0 deps** |
| Go | 69 | 527 | 1.36× | 329 | 1.13× | 14.8 MB binary + 1 module dep |
| Python | 42 | 383 | 0.99× | 332 | 1.14× | source + CPython interpreter |

† *edit* = applying one concrete, equal-semantics change (add a `created_at`
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
