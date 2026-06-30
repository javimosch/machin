#!/usr/bin/env python3
# Generate random, TYPE-CORRECT main-only programs in sub-slice-2's supported grammar.
# `encode` only accepts well-typed programs, so the fuzzer must stay type-correct.
# Covers: scalars (int/float/string/bool) with all operators, slice literals + indexing,
# for-range over slices and strings, if/while. (Maps/structs/channels are covered by the
# hand-written battery in verify-check2.sh.) Each var has a stable type; no int/float mix.
import random, sys

SCALARS = ("int", "float", "string", "bool")

def gen(seed, nstmts):
    r = random.Random(seed)
    V = {t: [] for t in SCALARS}     # scalar vars by type
    S = {t: [] for t in SCALARS}     # slice vars by element type
    lines = ["func main() {"]
    ctr = [0]
    def fresh():
        n = f"v{ctr[0]}"; ctr[0] += 1; return n

    def lit(t):
        if t == "int": return str(r.randrange(0, 100))
        if t == "float": return f"{r.randrange(0,50)}.{r.randrange(1,9)}"
        if t == "string": return '"' + r.choice(["a", "bc", "hi", "x", ""]) + '"'
        return r.choice(["true", "false"])

    def ex(t, depth):
        if depth <= 0 or r.random() < 0.35:
            roll = r.random()
            if V[t] and roll < 0.5:
                return r.choice(V[t])
            if S[t] and roll < 0.7:                       # index a slice of element type t
                return f"{r.choice(S[t])}[{ex('int', 0)}]"
            return lit(t)
        if t == "int":
            op = r.choice(["+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>"])
            return f"({ex('int', depth-1)} {op} {ex('int', depth-1)})"
        if t == "float":
            op = r.choice(["+", "-", "*", "/"])
            return f"({ex('float', depth-1)} {op} {ex('float', depth-1)})"
        if t == "string":
            if r.random() < 0.6:
                return f"({ex('string', depth-1)} + {ex('string', depth-1)})"
            return lit("string")
        k = r.random()
        if k < 0.3:
            return f"!({ex('bool', depth-1)})"
        if k < 0.55:
            return f"({ex('bool', depth-1)} {r.choice(['&&','||'])} {ex('bool', depth-1)})"
        nt = r.choice(["int", "float", "string"])
        op = r.choice(["==", "!="]) if nt == "string" else r.choice(["==","!=","<","<=",">",">="])
        return f"({ex(nt, depth-1)} {op} {ex(nt, depth-1)})"

    def stmts(indent, budget):
        for _ in range(budget):
            roll = r.random()
            if roll < 0.12:
                lines.append(f"{indent}if {ex('bool', 2)} {{")
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.20:
                lines.append(f"{indent}while {ex('bool', 2)} {{")
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.30 and any(S[t] for t in SCALARS):
                et = r.choice([t for t in SCALARS if S[t]])
                sv = r.choice(S[et])
                iv, vv = fresh(), fresh()
                lines.append(f"{indent}for {iv}, {vv} := range {sv} {{")
                V["int"].append(iv); V[et].append(vv)
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.38 and V["string"]:
                sv = r.choice(V["string"]); iv = fresh()
                lines.append(f"{indent}for {iv} := range {sv} {{")
                V["int"].append(iv)
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.50 and any(V[t] for t in SCALARS):
                t = r.choice([t for t in SCALARS if V[t]])
                lines.append(f"{indent}{r.choice(V[t])} = {ex(t, 3)}")
            elif roll < 0.62 and any(S[t] for t in SCALARS):   # index-assign a slice
                et = r.choice([t for t in SCALARS if S[t]])
                lines.append(f"{indent}{r.choice(S[et])}[{ex('int',1)}] = {ex(et, 2)}")
            elif roll < 0.74:                                  # declare a slice
                et = r.choice(SCALARS)
                n = r.randrange(0, 4)
                elems = ", ".join(ex(et, 1) for _ in range(n))
                name = fresh()
                lines.append(f"{indent}{name} := []{et}{{{elems}}}")
                S[et].append(name)
            else:                                              # declare a scalar
                t = r.choice(SCALARS)
                name = fresh()
                lines.append(f"{indent}{name} := {ex(t, 3)}")
                V[t].append(name)

    stmts("    ", nstmts)
    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
