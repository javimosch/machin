# tools

Developer tooling for machin. Not part of the compiler build.

## tokcost.py

Measures the agent (LLM) **write/edit cost** of a source form, in output tokens,
using a real BPE tokenizer (`tiktoken`). machin's north star is *fast for machines
to write and edit* — and for an LLM that cost is dominated by output tokens — so
this is how we hold that claim accountable instead of guessing.

```bash
pip install tiktoken
python3 tools/tokcost.py examples
```

It decodes every example function and reports, for base64 vs equivalent plain
text:

- **WRITE** — tokens to emit the whole corpus in each form.
- **FUNC-REWRITE** — tokens to re-emit one function (the realistic edit unit).
- **LOCAL EDIT** — tokens for a one-character change (text emits a tiny diff;
  base64 must re-emit the whole encoded line).

Two encodings (`o200k_base`, `cl100k_base`) are reported so the result is shown
to be tokenizer-independent. They are a proxy for Claude's proprietary tokenizer,
but the *ratio* (base64 fragments ~2.5×) is universal across BPE tokenizers.

This is also the instrument for the real moat work: measuring whether a
syntax change actually lowers machine write/edit cost, rather than assuming it.

## tokmin.py

Finds *where* MFL spends tokens and measures what minimizations would save,
split by risk:

- **Class A — canonical whitespace** (zero risk): tightening `fib(n - 1)` →
  `fib(n-1)`. Measured ~13% corpus savings; now the canonical form (see
  `tighten` in `main.go`).
- **Class B — keyword/builtin renames** (weigh vs reliability): measured to be a
  dead end — `func`→`fn` saves 0 tokens (both are already single tokens) and
  `println`→`pln` is *worse* (abbreviations fragment in the tokenizer).

```bash
python3 tools/tokmin.py examples
```

The lesson it encodes: minimize tokens by removing what the tokenizer charges
for (whitespace), not by shortening what it already packs into one token
(common keywords).
