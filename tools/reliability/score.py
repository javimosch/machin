#!/usr/bin/env python3
"""reliability scorer — the second half of machin's write/edit-cost metric.

tokcost/tokmin measure tokens; they cannot see whether a syntax form makes an
agent get the code *wrong* more often. This does. Given model-written programs
for a bank of tasks in two surface forms (a control and a candidate), it
compiles and runs each, and reports per-form compile + correctness rates so a
token saving can be weighed against a reliability cost.

Usage:
    MACHIN=/path/to/machin python3 score.py <outdir>

<outdir> holds files named "<variant>_<trial>.txt", variant in {control,treat}.
Each file contains, for each task, a block:

    ### <task-id>
    <one MFL declaration per line>

For the `treat` (drop-func) variant, declarations omit the `func` keyword; the
scorer reinserts it (prepending `func ` to each non-`type` declaration line) so
the *current* compiler can build it — isolating the question "does dropping the
marker make the model write worse logic?" from "is the grammar implemented yet?"
It also reports func-adherence: how often the model actually dropped `func`.
"""
import glob
import json
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
MACHIN = os.environ.get("MACHIN", "machin")


def load_tasks():
    with open(os.path.join(HERE, "tasks.json")) as f:
        return {t["id"]: t for t in json.load(f)}


def parse_blocks(text):
    """Split a model output file into {task-id: program-text}."""
    blocks, cur, buf = {}, None, []
    for line in text.splitlines():
        m = re.match(r"^\s*#{2,3}\s*([\w-]+)\s*$", line)
        if m:
            if cur:
                blocks[cur] = "\n".join(buf).strip()
            cur, buf = m.group(1), []
        elif cur:
            buf.append(line)
    if cur:
        blocks[cur] = "\n".join(buf).strip()
    return blocks


def canonicalize(program, variant):
    """Return (mfl_source, dropped_func_count, total_decls) ready to compile."""
    lines = [l.strip() for l in program.splitlines() if l.strip()]
    # ignore stray markdown fences the model may add
    lines = [l for l in lines if not l.startswith("```")]
    dropped = total = 0
    out = []
    for l in lines:
        if l.startswith("type "):
            out.append(l)
            continue
        total += 1
        if l.startswith("func "):
            out.append(l)  # already has the marker
        else:
            if variant == "treat":
                dropped += 1
            out.append("func " + l)  # reinsert for the current grammar
    return "\n".join(out) + "\n", dropped, total


def run(src):
    """Compile+run an MFL source string; return (compiled, stdout)."""
    path = os.path.join(os.environ.get("TMPDIR", "/tmp"), "_rel.mfl")
    with open(path, "w") as f:
        f.write(src)
    try:
        p = subprocess.run([MACHIN, "run", path], input=b"",
                           capture_output=True, timeout=30)
    except Exception:
        return False, ""
    return p.returncode == 0, p.stdout.decode("utf-8", "replace")


def main():
    outdir = sys.argv[1] if len(sys.argv) > 1 else "/tmp/rel/out"
    tasks = load_tasks()
    agg = {}  # variant -> counters
    for path in sorted(glob.glob(os.path.join(outdir, "*.txt"))):
        variant = os.path.basename(path).split("_")[0]
        a = agg.setdefault(variant, dict(n=0, compiled=0, correct=0, dropped=0, decls=0, missing=0))
        blocks = parse_blocks(open(path).read())
        for tid, task in tasks.items():
            a["n"] += 1
            prog = blocks.get(tid, "")
            if not prog:
                a["missing"] += 1
                continue
            src, dropped, total = canonicalize(prog, variant)
            a["dropped"] += dropped
            a["decls"] += total
            compiled, out = run(src)
            if compiled:
                a["compiled"] += 1
            if compiled and out == task["expected"]:
                a["correct"] += 1

    print(f"task bank: {len(tasks)} tasks | binary: {MACHIN}\n")
    print(f"{'variant':9} {'n':>3} {'compile%':>9} {'correct%':>9} {'func-dropped%':>14}")
    for variant in ("control", "treat"):
        a = agg.get(variant)
        if not a:
            continue
        n = a["n"] or 1
        comp = 100 * a["compiled"] / n
        corr = 100 * a["correct"] / n
        adher = 100 * a["dropped"] / (a["decls"] or 1) if variant == "treat" else float("nan")
        adher_s = "      n/a" if variant == "control" else f"{adher:13.0f}%"
        print(f"{variant:9} {a['n']:>3} {comp:8.0f}% {corr:8.0f}% {adher_s:>14}")

    c, t = agg.get("control"), agg.get("treat")
    if c and t:
        dc = 100 * c["correct"] / (c["n"] or 1)
        dt = 100 * t["correct"] / (t["n"] or 1)
        print(f"\nΔ correctness (treat - control): {dt - dc:+.1f} points")
        print("interpretation: a negative Δ is the reliability cost of the form;"
              " weigh it against the token saving (drop-func ≈ -1.7%).")


if __name__ == "__main__":
    main()
