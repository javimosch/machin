#!/usr/bin/env python3
"""Fail if `machin test --cover --json` reports coverage below a floor.

Mirrors the Go side's TestTypesCoverageFloor: a ratchet, not a target. The floors
are set just under what the suites measure TODAY, so they catch a regression
without pretending to a number the repo has not earned.

Two floors, because the two blocks measure different things:

  --func   function coverage OF THE MODULE UNDER TEST. Scoped to one file on
           purpose: the composed program also contains framework/test.src (the
           assert helpers) and the test file itself, and a suite that happens not
           to call assert_eq_str must not fail a gate about the module it tests.

  --stmt   statement coverage of the WHOLE composed program. `machin test --cover`
           reports statements globally (per owning function, not per file), so
           this floor is necessarily coarser and is set lower to reflect that.
           Tighten it when statement coverage grows a per-file breakdown.

Usage:
  machin test --cover --json <module> <suite> | cov-floor.py --module <module> \
      --func 90 --stmt 75
"""
import argparse
import json
import sys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--module", required=True, help="path of the module under test")
    ap.add_argument("--func", type=float, required=True, help="function coverage floor %")
    ap.add_argument("--stmt", type=float, default=0.0, help="statement coverage floor %")
    args = ap.parse_args()

    try:
        report = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        print(f"cov-floor: could not parse `machin test --cover --json` output: {e}", file=sys.stderr)
        return 2

    cov = report.get("coverage")
    if not cov:
        print("cov-floor: no coverage block — was --cover passed?", file=sys.stderr)
        return 2

    # A missing module entry is a hard error, not a pass. Silently skipping it is
    # how a gate stops guarding anything: rename the file and the floor evaporates.
    entry = next((f for f in cov.get("files", []) if f["file"] == args.module), None)
    if entry is None:
        seen = ", ".join(f["file"] for f in cov.get("files", []))
        print(f"cov-floor: {args.module} absent from the report (saw: {seen})", file=sys.stderr)
        return 2

    failures = []
    fn_pct = entry["covered"] / entry["total"] * 100 if entry["total"] else 100.0
    print(f"  {args.module}: function {entry['covered']}/{entry['total']} "
          f"({fn_pct:.1f}%), floor {args.func:.0f}%")
    if entry["uncovered"]:
        print(f"    uncovered: {', '.join(entry['uncovered'])}")
    if fn_pct + 1e-9 < args.func:
        failures.append(f"function coverage {fn_pct:.1f}% is below the {args.func:.0f}% floor")

    if args.stmt > 0:
        sc = cov.get("statements")
        if not sc:
            print("cov-floor: no statement block in the report", file=sys.stderr)
            return 2
        print(f"  whole program: statement {sc['covered']}/{sc['total']} "
              f"({sc['pct']:.1f}%), floor {args.stmt:.0f}%")
        if sc["pct"] + 1e-9 < args.stmt:
            failures.append(f"statement coverage {sc['pct']:.1f}% is below the {args.stmt:.0f}% floor")

    if failures:
        for f in failures:
            print(f"cov-floor: FAIL — {f}", file=sys.stderr)
        print("cov-floor: add tests, or lower the floor deliberately and say why in the commit.",
              file=sys.stderr)
        return 1
    print("  cov-floor: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
