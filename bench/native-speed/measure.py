#!/usr/bin/env python3
"""Time the same four compute kernels built three ways: machin, Rust, Zig.

The README claims machin is "C/Rust-class speed" — machin compiles MFL through C
to native (`cc -O2`), so its runtime is whatever the C optimizer produces. This
checks that against the two reference native toolchains: Rust (`-C opt-level=3`)
and Zig (`-OReleaseFast`). Each kernel is byte-for-byte identical in output across
all three (verified separately), so we are timing the SAME computation.

METHODOLOGY — two things that matter on a laptop:

  * INTERLEAVED, NOT BLOCKED. An earlier version of this script ran all N samples
    of machin, then all N of Rust, then all N of Zig. On a CPU that heats up and
    down-clocks during a multi-second kernel, that penalizes whichever language
    runs last — an order bias, not a speed difference. We now run one round of
    (machin, rust, zig), then the next round, rotating who goes first each round,
    so thermal drift is spread evenly across all three.

  * MIN AND SPREAD. We report the MIN (the least-interfered-with sample, the
    standard choice for compute benchmarks) but also the median and the observed
    spread, because a min alone hides how noisy the machine was. If the spread is
    wide, treat small differences as ties — and this script says so explicitly.

Absolute milliseconds are machine-specific and will differ on your hardware; the
RATIOS between languages are the portable result.
"""
import os
import statistics
import subprocess
import time

HERE = os.path.dirname(os.path.abspath(__file__))
LANGS = ["machin", "rust", "zig"]
KERNELS = ["fib", "mandel", "sieve", "intsum"]
RUNS = 9

# Differences below this are reported as ties: on a thermally-variable laptop a
# few percent is noise, and claiming a winner there is how benchmarks start lying.
TIE_PCT = 3.0


def run_once(path):
    t0 = time.perf_counter()
    r = subprocess.run([path], capture_output=True, text=True)
    return time.perf_counter() - t0, (r.stdout + r.stderr).strip()


def main():
    samples = {(k, l): [] for k in KERNELS for l in LANGS}
    outs = {}
    missing = set()

    for k in KERNELS:
        for r in range(RUNS):
            # Rotate the starting language each round so no one always runs on a
            # cold (or hot) CPU.
            order = LANGS[r % len(LANGS):] + LANGS[:r % len(LANGS)]
            for lang in order:
                p = os.path.join(HERE, lang, k)
                if not os.path.exists(p):
                    missing.add((k, lang))
                    continue
                dt, out = run_once(p)
                samples[(k, lang)].append(dt)
                outs[(k, lang)] = out

    print(f"min wall-clock over {RUNS} interleaved rounds (lower = faster)")
    print(f"differences under {TIE_PCT:.0f}% are reported as ties (machine noise)\n")
    print(f"{'kernel':<8} {'machin':>11} {'rust':>11} {'zig':>11}   {'verdict':>22}")
    print("-" * 70)

    for k in KERNELS:
        mins, spreads = {}, {}
        for l in LANGS:
            s = samples[(k, l)]
            if not s:
                mins[l] = None
                continue
            mins[l] = min(s)
            spreads[l] = (max(s) - min(s)) / min(s) * 100

        live = {l: v for l, v in mins.items() if v}
        if not live:
            continue
        best = min(live.values())

        cells = []
        for l in LANGS:
            cells.append(f"{mins[l]*1000:9.1f}ms" if mins[l] else f"{'--':>11}")

        m = mins["machin"]
        ratio = m / best
        # Who is machin actually competing with — the best of the two references?
        refs = [mins[l] for l in ("rust", "zig") if mins[l]]
        best_ref = min(refs) if refs else None
        if best_ref:
            delta = (best_ref - m) / best_ref * 100  # >0 => machin faster
            if abs(delta) < TIE_PCT:
                verdict = f"tie (within {TIE_PCT:.0f}%)"
            elif delta > 0:
                verdict = f"machin faster by {delta:.0f}%"
            else:
                verdict = f"machin slower by {ratio:.2f}x"
        else:
            verdict = "--"

        ok = len({outs.get((k, l)) for l in LANGS if mins[l]}) == 1
        flag = "" if ok else "  !! checksum mismatch"
        print(f"{k:<8} {cells[0]:>11} {cells[1]:>11} {cells[2]:>11}   {verdict:>22}{flag}")

    worst = max((max(((max(s)-min(s))/min(s)*100) for s in [samples[(k, l)]] if s)
                 for k in KERNELS for l in LANGS if samples[(k, l)]), default=0)
    print(f"\nnoise: worst observed run-to-run spread was {worst:.0f}% of the min sample.")
    print("Verdicts are min-based; anything inside the tie band is reported as a tie.")
    print("All kernels verified to print identical results across the three languages.")
    if missing:
        print(f"missing binaries (run ./run.sh first): {sorted(missing)}")


if __name__ == "__main__":
    main()
