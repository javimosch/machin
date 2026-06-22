# reliability harness

The second half of machin's write/edit-cost metric. `tokcost`/`tokmin` measure
**tokens**; they cannot see whether a syntax form makes an agent get the code
*wrong* more often. This measures that. The real cost is `tokens × tries`, so a
token saving that raises the error rate can be a net loss — this harness is how
we tell.

## How it works

1. **Task bank** — `tasks.json`: small programs with a precise spec and the
   exact expected stdout.
2. **Generate** — a model (run as independent subagents) writes a solution to
   each task in two surface forms: a **control** and a **candidate**, given an
   identical MFL primer that differs *only* in the rule under test. Generation
   is first-try: no repo access, no compiler feedback.
3. **Score** — `score.py` compiles and runs every program and reports, per form,
   the compile rate, the correctness rate (exact stdout match), and (for the
   candidate) how often the model actually adopted the change.

```bash
go build -o /tmp/machin .
MACHIN=/tmp/machin python3 tools/reliability/score.py <outdir>
```

Output files are named `<variant>_<trial>.txt`, each holding `### <task-id>`
blocks. For a candidate that *removes* a construct the current compiler still
needs (e.g. drop-`func`), the scorer reinserts it mechanically before compiling,
so it measures "does dropping the marker make the model write worse logic?"
separately from "is the new grammar implemented yet?".

## Experiment 1 — drop the `func` keyword (issue #101)

Hypothesis: removing the `func` marker (−1.7% tokens) might hurt reliability —
the model could write worse logic, or keep emitting `func` out of habit.

13 tasks × 3 trials × 2 forms = **78 programs** (frontier model):

| form | n | compile | correct | func-dropped |
|------|---|---------|---------|--------------|
| control (`func name(){}`) | 39 | 100% | 100% | — |
| treat (`name(){}`)        | 39 | 100% | 100% | 100% |

**Δ correctness: 0.0 points.** No measurable reliability cost, and full
adherence (the model never fought the instruction). Verdict: drop-`func` is
de-risked at this model capability.

**Limitation — ceiling effect.** Both forms scored 100%, so the suite can't
resolve a *small* penalty; it rules out a large one. A definitive answer would
need a harder bank (base error rate > 0) or a weaker model. The honest reading:
no evidence of a reliability cost, strong evidence of compliance — but the win
itself is small (1.7%), so whether to spend a language change on it is a product
call (see issue #101's open question on how far from in-distribution to go).
