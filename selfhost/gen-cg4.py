#!/usr/bin/env python3
# Stage-4d (part 1) codegen fuzz: TYPE-CORRECT programs exercising aggregates —
# struct literals/field access/field-assign, slice literals/index/index-assign/range,
# map make/set/get/range, string range. Diffed as C against cgentest.
import random, sys

def gen(seed, nstmts):
    r = random.Random(seed)
    lines = ["type Pt struct { x int  y int  name string }", "func main() {"]
    ints, flts, strs, bools = [], [], [], []
    islices, sslices = [], []   # []int, []string vars
    pts = []                    # Pt struct vars
    imaps = []                  # map[string]int vars
    ctr = [0]
    def fresh():
        n = f"v{ctr[0]}"; ctr[0] += 1; return n
    def ival():
        pool = ints + ([f"{r.choice(pts)}.x" for _ in range(1)] if pts else [])
        pool += ([f"{r.choice(islices)}[0]" for _ in range(1)] if islices else [])
        if pool and r.random() < 0.6: return r.choice(pool)
        return str(r.randrange(0, 100))
    def sval():
        if strs and r.random() < 0.5: return r.choice(strs)
        if pts and r.random() < 0.3: return f"{r.choice(pts)}.name"
        return '"' + r.choice(["a", "bc", "x"]) + '"'
    for _ in range(nstmts):
        c = r.random()
        if c < 0.16:
            n = fresh(); lines.append(f"    {n} := {ival()}"); ints.append(n)
        elif c < 0.28:
            n = fresh(); lines.append(f"    {n} := {sval()}"); strs.append(n)
        elif c < 0.42:
            n = fresh(); lines.append(f"    {n} := Pt{{x: {ival()}, y: {ival()}, name: {sval()}}}"); pts.append(n)
        elif c < 0.5 and pts:
            p = r.choice(pts); lines.append(f"    {p}.x = {ival()}")
        elif c < 0.58 and pts:
            p = r.choice(pts); lines.append(f"    {p}.name = {sval()}")
        elif c < 0.7:
            n = fresh(); k = r.randrange(0, 4)
            lines.append(f"    {n} := []int{{{', '.join(ival() for _ in range(k))}}}"); islices.append(n)
        elif c < 0.78 and islices:
            s = r.choice(islices); lines.append(f"    {s}[0] = {ival()}")
        elif c < 0.88 and islices:
            s = r.choice(islices); iv, vv = fresh(), fresh()
            lines.append(f"    acc{ctr[0]} := 0")
            acc = f"acc{ctr[0]}"; ctr[0]+=1; ints.append(acc)
            lines.append(f"    for {iv}, {vv} := range {s} {{ {acc} = {acc} + {vv} + {iv} }}")
            ints.append(iv); ints.append(vv)
        elif c < 0.94:
            n = fresh(); lines.append(f"    {n} := make(map[string]int)"); imaps.append(n)
        elif imaps:
            mp = r.choice(imaps)
            if r.random() < 0.5:
                lines.append(f"    {mp}[{sval()}] = {ival()}")
            else:
                n = fresh(); lines.append(f"    {n} := {mp}[{sval()}]"); ints.append(n)
        else:
            n = fresh(); lines.append(f"    {n} := {ival()}"); ints.append(n)
    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
