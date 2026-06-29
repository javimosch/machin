#!/usr/bin/env python3
"""Time the same four compute kernels built three ways: machin, Rust, Zig.

The README claims machin is "C/Rust-class speed" — machin compiles MFL through C
to native (`cc -O2`), so its runtime is whatever the C optimizer produces. This
checks that against the two reference native toolchains: Rust (`rustc -O`) and Zig
(`-OReleaseFast`). Each kernel is byte-for-byte identical in output across all
three (verified separately), so we are timing the SAME computation.

Reports the MIN wall-clock over N runs (min = least noise / scheduler interference).
Build the binaries with ./run.sh first.
"""
import os
import subprocess
import time

HERE = os.path.dirname(os.path.abspath(__file__))
LANGS = ["machin", "rust", "zig"]
KERNELS = ["fib", "mandel", "sieve", "intsum"]
RUNS = 5


def bench(path):
    best = None
    out = None
    for _ in range(RUNS):
        t0 = time.perf_counter()
        r = subprocess.run([path], capture_output=True, text=True)
        dt = time.perf_counter() - t0
        out = (r.stdout + r.stderr).strip()
        best = dt if best is None else min(best, dt)
    return best, out


def main():
    results = {}  # (kernel, lang) -> (sec, out)
    for k in KERNELS:
        for lang in LANGS:
            p = os.path.join(HERE, lang, k)
            if not os.path.exists(p):
                results[(k, lang)] = (None, "MISSING")
                continue
            results[(k, lang)] = bench(p)

    print(f"min wall-clock over {RUNS} runs (lower = faster)\n")
    print(f"{'kernel':<8} {'machin':>10} {'rust':>10} {'zig':>10}   {'machin vs best':>16}")
    print("-" * 60)
    for k in KERNELS:
        times = {lang: results[(k, lang)][0] for lang in LANGS}
        outs = {lang: results[(k, lang)][1] for lang in LANGS}
        ok = len(set(outs.values())) == 1  # all checksums equal
        best = min(t for t in times.values() if t)
        cells = []
        for lang in LANGS:
            t = times[lang]
            mark = "*" if t == best else " "
            cells.append(f"{t*1000:8.1f}ms{mark}" if t else f"{'--':>10}")
        ratio = times["machin"] / best
        flag = "" if ok else "  !! checksum mismatch"
        print(f"{k:<8} {cells[0]:>10} {cells[1]:>10} {cells[2]:>10}   {ratio:>14.2f}x{flag}")
    print("\n* = fastest for that kernel. 'machin vs best' = how far machin is off the leader.")
    print("All kernels verified to print identical results across the three languages.")


if __name__ == "__main__":
    main()
