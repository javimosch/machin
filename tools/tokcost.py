#!/usr/bin/env python3
"""tokcost — measure the agent (LLM) write/edit cost of MFL's base64 source form
vs the equivalent plain canonical text, using a real BPE tokenizer (tiktoken).

The question this answers: machin's north star is "fast for machines to write and
edit." For an LLM agent, that cost is dominated by OUTPUT TOKENS. This harness
tokenizes every example function in both forms and reports the ratio.

Metrics (all assumption-free except the clearly-labelled locality demo):

  WRITE       tokens to emit the whole program in each form.
  FUNC-REWRITE tokens to re-emit one full function (the realistic edit unit,
              since one function = one line). No edit-type assumption: it is just
              the encoding tax applied to a function body.
  LOCAL EDIT  (illustrative, one function) a minimal change — e.g. a literal
              `2` -> `3`. In plain text the agent emits a tiny old->new diff;
              base64 has no locality, so ANY change forces re-emitting the whole
              encoded line.

Tokenizers are a proxy for Claude's (which is proprietary), but the result —
base64 fragments far worse than code — is universal across BPE tokenizers; we
report o200k_base and cl100k_base so you can see it is not encoding-specific.
"""
import base64
import glob
import os
import sys

import tiktoken

ENCODINGS = ["o200k_base", "cl100k_base"]


def decode_funcs(path):
    """Yield (b64, text) for each declaration, deriving both forms from whichever
    the .mfl uses: canonical plain text always contains whitespace; a packed
    (base64) line never does. Works whether the repo stores text or base64."""
    out = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            if any(c.isspace() for c in line):
                text = line  # plain canonical text
                b64 = base64.b64encode(text.encode()).decode()
            else:
                try:
                    text = base64.b64decode(line).decode()  # packed form
                except Exception:
                    continue
                b64 = line
            out.append((b64, text))
    return out


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else "examples"
    files = sorted(glob.glob(os.path.join(root, "**", "*.mfl"), recursive=True))
    if not files:
        print(f"no .mfl files under {root}", file=sys.stderr)
        sys.exit(1)

    funcs = []  # (file, b64, text)
    for path in files:
        for b64, text in decode_funcs(path):
            funcs.append((path, b64, text))

    print(f"corpus: {len(files)} files, {len(funcs)} functions\n")

    for enc_name in ENCODINGS:
        enc = tiktoken.get_encoding(enc_name)
        tok = lambda s: len(enc.encode(s))

        # WRITE: whole corpus in each form.
        w_text = sum(tok(t) for _, _, t in funcs)
        w_b64 = sum(tok(b) for _, b, _ in funcs)

        # FUNC-REWRITE: per-function token cost, averaged.
        ratios = [tok(b) / tok(t) for _, b, t in funcs if tok(t)]
        avg_ratio = sum(ratios) / len(ratios)
        worst = max(funcs, key=lambda f: tok(f[1]) / max(tok(f[2]), 1))

        print(f"== {enc_name} ==")
        print(f"  WRITE whole corpus:   text {w_text:>6} tok | base64 {w_b64:>6} tok"
              f"  -> base64 costs {w_b64 / w_text:.2f}x")
        print(f"  FUNC-REWRITE (avg):   base64 / text = {avg_ratio:.2f}x per function")
        print(f"  worst function:       {worst[0]}  ({tok(worst[1]) / tok(worst[2]):.2f}x)")
        print()

    # LOCAL EDIT demo (encoding-independent structural point), with o200k.
    enc = tiktoken.get_encoding("o200k_base")
    tok = lambda s: len(enc.encode(s))
    demo = None
    for path, b64, text in funcs:
        if text.startswith("func fib("):
            demo = (path, b64, text)
            break
    if demo:
        path, b64, text = demo
        # a one-character change: the base case `n < 2` -> `n < 3`
        old, new = "< 2", "< 3"
        text_edit = tok(old) + tok(new)          # what an Edit(old_string,new_string) emits
        b64_edit = tok(base64.b64encode(text.replace(old, new).encode()).decode())
        print("== LOCAL EDIT demo (change `n < 2` -> `n < 3` in fib) ==")
        print(f"  plain text: emit old->new diff  = {text_edit} tok")
        print(f"  base64:     re-emit whole line  = {b64_edit} tok"
              f"   -> {b64_edit / text_edit:.0f}x more for a one-character change")


if __name__ == "__main__":
    main()
