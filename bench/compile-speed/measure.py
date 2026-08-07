#!/usr/bin/env python3
"""Time the BUILD, not the run: machin vs Rust vs Zig on identical kernels.

`bench/native-speed` answers "how fast does the program run?". This answers the
question a developer (or an agent in a write->build->fix loop) waits on dozens of
times an hour: "how long until I have a binary, and how big is it?"

  machin : machin build            (generates C, then cc -O2)
  Rust   : rustc -C opt-level=3    (Rust's real release level)
  Zig    : zig build-exe -OReleaseFast

FAIRNESS:

  * Rust is invoked as bare `rustc` on a single file, NOT `cargo build`. That
    strips cargo's manifest/lockfile/dependency-resolution overhead, which would
    inflate Rust's number for reasons that have nothing to do with the compiler.
    This is the most generous fair setting for Rust — and Rust still wins.
  * machin's number INCLUDES its C backend run (`cc -O2`) — the whole pipeline to
    a runnable binary, not just its own frontend.
  * `machin encode` (loose .src -> canonical .mfl) is authoring, not compiling, and
    is done once outside the timed region.
  * Sizes are reported BOTH as-produced and STRIPPED, with linkage, because
    "as-produced" mostly measures how much debug info each toolchain leaves in.
    Comparing a dynamically-linked binary to a statically-linked one is not a
    like-for-like comparison, so the linkage is printed next to every number.

ZIG CAVEAT: on the machine this was developed on (zig 0.16.0 beta, snap), every
`-OReleaseFast` build that touches `std.debug.print` costs ~13 s and is NOT reused
between identical invocations, while the same program in Debug costs 0.5 s and a
program that prints nothing costs 0.22 s. That is the cost of compiling std's
formatting under ReleaseFast, apparently uncached on this build. It says little
about how Zig scales with YOUR code, so this harness prints Zig's number but draws
no conclusion from it.
"""
import os
import shutil
import subprocess
import time

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.join(os.path.dirname(HERE), "native-speed")
OUT = os.path.join(HERE, "build")
KERNELS = ["fib", "mandel", "sieve", "intsum"]
LANGS = ["machin", "rust", "zig"]
RUNS = 3


def timed(cmd, cwd=None):
    t0 = time.perf_counter()
    r = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    return time.perf_counter() - t0, r.returncode == 0


def build_machin(k):
    mfl, out = os.path.join(OUT, f"{k}.mfl"), os.path.join(OUT, f"machin-{k}")
    if not os.path.exists(mfl):
        with open(mfl, "w") as f:
            subprocess.run(["machin", "encode", os.path.join(SRC, "machin", f"{k}.src")],
                           stdout=f, stderr=subprocess.DEVNULL)
    if os.path.exists(out):
        os.remove(out)
    return timed(["machin", "build", mfl, "-o", out]), out


def build_rust(k):
    out = os.path.join(OUT, f"rust-{k}")
    if os.path.exists(out):
        os.remove(out)
    return timed(["rustc", "-C", "opt-level=3",
                  os.path.join(SRC, "rust", f"{k}.rs"), "-o", out]), out


def build_zig(k):
    out = os.path.join(OUT, f"zig-{k}")
    for c in (".zig-cache", "zig-cache"):
        shutil.rmtree(os.path.join(OUT, c), ignore_errors=True)
    for p in (out, out + ".o"):
        if os.path.exists(p):
            os.remove(p)
    return timed(["zig", "build-exe", "-OReleaseFast",
                  os.path.join(SRC, "zig", f"{k}.zig"),
                  "-femit-bin=" + out], cwd=OUT), out


BUILDERS = {"machin": build_machin, "rust": build_rust, "zig": build_zig}


def stripped_size(path):
    """Size after `strip`, measured on a copy so the original is left alone."""
    tmp = path + ".stripped"
    try:
        shutil.copy(path, tmp)
        subprocess.run(["strip", tmp], capture_output=True)
        return os.path.getsize(tmp)
    except Exception:
        return None
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)


def linkage(path):
    r = subprocess.run(["ldd", path], capture_output=True, text=True)
    if "not a dynamic executable" in (r.stdout + r.stderr):
        return "static"
    return "dynamic"


def main():
    os.makedirs(OUT, exist_ok=True)
    times, raw, stripped, link = {}, {}, {}, {}

    for k in KERNELS:
        for lang in LANGS:
            best, ok_all, path = None, True, None
            for _ in range(RUNS):
                (dt, ok), path = BUILDERS[lang](k)
                ok_all &= ok
                best = dt if best is None else min(best, dt)
            good = ok_all and path and os.path.exists(path)
            times[(k, lang)] = best if good else None
            raw[(k, lang)] = os.path.getsize(path) if good else None
            stripped[(k, lang)] = stripped_size(path) if good else None
            link[(k, lang)] = linkage(path) if good else "?"

    print(f"BUILD TIME — source to optimized native binary, min of {RUNS} builds\n")
    print(f"{'kernel':<8} {'machin':>10} {'rust':>10} {'zig':>10}")
    print("-" * 42)
    for k in KERNELS:
        cells = []
        for l in LANGS:
            t = times[(k, l)]
            cells.append(f"{t*1000:8.0f}ms" if t else f"{'FAIL':>10}")
        print(f"{k:<8} {cells[0]:>10} {cells[1]:>10} {cells[2]:>10}")
    print("\nRust wins this outright on single-file programs; machin's number "
          "includes\nits cc -O2 backend run. See the ZIG CAVEAT in this file's "
          "docstring before\nreading anything into Zig's column.")

    print(f"\n\nBINARY SIZE — as produced, and stripped (linkage in brackets)\n")
    print(f"{'kernel':<8} {'machin':>22} {'rust':>22} {'zig':>22}")
    print("-" * 78)
    for k in KERNELS:
        cells = []
        for l in LANGS:
            r, s = raw[(k, l)], stripped[(k, l)]
            cells.append(f"{r//1024}k / {s//1024}k [{link[(k,l)][:3]}]" if r and s else "FAIL")
        print(f"{k:<8} {cells[0]:>22} {cells[1]:>22} {cells[2]:>22}")
    print("\nformat: as-produced / stripped [dyn|sta]")
    print("Compare like with like: machin and rust are both dynamic, so their "
          "stripped\nnumbers are directly comparable. Zig links statically here, "
          "so it carries libc\nthat the other two borrow from the system.")


if __name__ == "__main__":
    main()
