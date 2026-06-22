#!/usr/bin/env python3
"""tokmin — find where MFL spends agent tokens, and measure what minimizations
would save. Companion to tokcost.py; same north star (low agent write/edit cost).

Two classes of minimization, measured separately because their risk differs:

  CLASS A — canonical whitespace.  Whitespace is insignificant to the lexer
    (except as a token separator), so the canonical form's spacing is a free
    variable. Tightening `fib(n - 1)` -> `fib(n-1)` is ZERO semantic risk and
    fully in-distribution. Implemented in normalize(), it changes nothing but
    token count.

  CLASS B — keyword / builtin renames.  `func` -> `fn`, etc. A real language
    change. Shorter is not automatically better: a token an LLM emits fluently
    (`func`, `return`) may cost the same or fewer tokens than a cryptic
    abbreviation that falls out of distribution and hurts reliability. So we
    MEASURE the token delta and weigh it against that risk per candidate.

All edits are applied OUTSIDE string literals only (string contents are
preserved), mirroring how a real string-aware normalizer would behave.
"""
import base64
import glob
import os
import re
import sys

import tiktoken

ENC = "o200k_base"
PUNCT = r"(){}\[\],+\-*/%<>=!&|;:"


def load_corpus(root):
    """All declarations as plain text (decode any packed base64 lines)."""
    texts = []
    for path in sorted(glob.glob(os.path.join(root, "**", "*.mfl"), recursive=True)):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                if not any(c.isspace() for c in line):
                    try:
                        line = base64.b64decode(line).decode()
                    except Exception:
                        continue
                texts.append(line)
    return texts


def split_strings(s):
    """Split into (segment, is_string) parts so edits skip string literals."""
    parts, i, n = [], 0, len(s)
    while i < n:
        j = s.find('"', i)
        if j < 0:
            parts.append((s[i:], False))
            break
        parts.append((s[i:j], False))
        k = j + 1
        while k < n and s[k] != '"':
            if s[k] == "\\":
                k += 1
            k += 1
        parts.append((s[j:k + 1], True))
        i = k + 1
    return parts


def edit_outside_strings(s, fn):
    return "".join(seg if is_str else fn(seg) for seg, is_str in split_strings(s))


def tighten_ws(s):
    return edit_outside_strings(s, lambda seg: re.sub(rf" *([{PUNCT}]) *", r"\1", seg))


def rename(s, old, new):
    return edit_outside_strings(s, lambda seg: re.sub(rf"\b{re.escape(old)}\b", new, seg))


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else "examples"
    texts = load_corpus(root)
    enc = tiktoken.get_encoding(ENC)
    tok = lambda s: len(enc.encode(s))

    base = sum(tok(t) for t in texts)
    print(f"corpus: {len(texts)} declarations, {base} tokens baseline ({ENC})\n")

    def report(label, transform, note=""):
        total = sum(tok(transform(t)) for t in texts)
        saved = base - total
        pct = 100 * saved / base
        tail = f"   {note}" if note else ""
        print(f"  {label:28} {total:>5} tok  ({saved:+5} = {pct:+5.1f}%){tail}")

    print("== CLASS A — canonical whitespace (zero risk) ==")
    report("tighten spacing", tighten_ws)
    print()

    print("== CLASS B — keyword / builtin renames (weigh vs reliability) ==")
    renames = [
        ("func", "fn", "common (Rust/Swift) — in distribution"),
        ("return", "ret", "less common — mild risk"),
        ("println", "pln", "builtin — abbreviation, some risk"),
        ("print", "pr", "builtin — abbreviation, some risk"),
        ("while", "loop", "neutral length — illustrative"),
        ("append", "push", "neutral-ish — semantics-preserving name"),
    ]
    for old, new, note in renames:
        report(f"{old} -> {new}", lambda s, o=old, n=new: rename(s, o, n), note)
    print()

    print("== COMBINED — whitespace + the safer renames (func->fn) ==")
    report("tighten + func->fn",
           lambda s: rename(tighten_ws(s), "func", "fn"))


if __name__ == "__main__":
    main()
