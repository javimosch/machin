# The self-certifying compiler (`machin certify`)

Every guarantee machin makes — race-freedom, falsification, replay — quietly assumes
one thing: **the compiler faithfully implemented your source.** `rustc` can't prove
that (it has miscompilation bugs); Zig can't either. `machin certify` checks that
assumption, per build.

```
machin certify app.mfl
```

## What it does

For each function, machin runs it two ways over the same bounded input space and
confirms they agree:

- **the source semantics** — machin's own concrete interpreter (the very evaluator the
  [Falsifier](falsify) uses), which *is* the meaning of your MFL; and
- **the codegen semantics** — the actually-compiled binary.

Every `(function, input)` pair is batched into one harness that prints
`json(fn(args))`, compiled and run once; the output is diffed line-by-line against the
interpreter's value. `json()` is one canonical serialization for every type, and the
interpreter side (`renderCanonical`) reproduces it byte-for-byte (floats use `%.6g` to
match C's `%g`, so a formatting quirk is never mistaken for a bug).

A divergence is a **TRV001 miscompilation** — the compiler produced something other
than what the source means — reported with the exact input and both values:

```
  MISCOMPILED  scale: scale(-3, -3) = 9 (source) but the compiler produced 0 (tried 25)
certify: MISCOMPILATION found — the compiler diverged from the source.
```

`machin certify` exits non-zero when it finds one.

## Verdicts

| Verdict | Meaning |
|---|---|
| `certified` | finite input domain, whole space enumerated, every input agreed |
| `certified-bounded` | agreed, but over a bounded (int/slice) domain — validated *up to the bounds* |
| `partial` | agreed on the modeled inputs; some inputs weren't modeled |
| `unknown` | not validatable — e.g. a return `json()` can't serialize, or a body the interpreter doesn't model |
| `miscompiled` | a real source-vs-codegen divergence |

## Honest by design

Same discipline as the Falsifier: a clean result means **"no miscompilation found
within the bounds,"** never "the compiler is proven correct." It's unsound-complete —
every divergence reported is a *real* discrepancy between source and codegen (no false
positives), but absence of a finding is not a proof of correctness.

Two consequences worth stating plainly:

- **Only instantiated functions are validated.** An uncalled function is dead code and
  never reaches the binary, so there's nothing to certify.
- **Coverage is bounded by what the interpreter models** — pure `int` / `[]int` /
  `bool` / `struct` / `string` logic (arithmetic, comparisons, bitwise, shifts,
  control flow, slice literals + `append` + index + `len` + `range`, and
  interprocedural calls). A function using floats-as-domain, bytes, maps, channels, or
  I/O comes back `unknown` — it is **never** falsely certified.

## What this is for

It turns "trust the compiler" — the assumption every language asks you to make — into
per-build evidence. Run it in CI, and a codegen regression that miscompiles any
validatable function fails the build with the exact input that exposes it. It is the
foundation the rest of machin's evidence stands on: race-freedom, falsification, and
replay are only as trustworthy as the binary faithfully implementing the source they
reason about.

*Rust proves your program is safe. Zig gives you control. machin proves the compiler
didn't lie to you.*
