#!/usr/bin/env python3
# Generate random, TYPE-CORRECT programs with USER-FUNCTION CALLS (single-return) for
# Stage-4b codegen. Fixed generic single-return helpers (int/float/string) are called —
# and nested — from main, exercising monomorphization + seqExprs side-effect ordering.
# No builtins / multi-return (those are later codegen slices).
import random, sys

HELPERS = """\
func idv(x) (r) { r = x }
func dbl(x) (r) { r = x + x }
func comb(a, b) (r) { r = a + b }
func neg(x) (r) { r = x - x - x }
"""

def gen(seed, nstmts):
    r = random.Random(seed)
    V = {"int": [], "float": [], "string": []}
    lines = [HELPERS, "func main() {"]
    ctr = [0]
    def fresh():
        n = f"v{ctr[0]}"; ctr[0] += 1; return n

    def lit(t):
        if t == "int": return str(r.randrange(0, 100))
        if t == "float": return f"{r.randrange(0,50)}.{r.randrange(1,9)}"
        return '"' + r.choice(["a", "bc", "x", ""]) + '"'

    def ex(t, depth):
        # produce an expression of type t, possibly a call (calls are the point)
        if depth <= 0 or r.random() < 0.35:
            if V[t] and r.random() < 0.5: return r.choice(V[t])
            return lit(t)
        roll = r.random()
        if roll < 0.28:
            return f"idv({ex(t, depth-1)})"
        if roll < 0.52:
            return f"dbl({ex(t, depth-1)})"
        if roll < 0.76:
            return f"comb({ex(t, depth-1)}, {ex(t, depth-1)})"
        if t != "string":
            # arithmetic (keeps type; string only supports +)
            op = r.choice(["+", "-", "*"]) if t == "int" else r.choice(["+", "-", "*"])
            return f"({ex(t, depth-1)} {op} {ex(t, depth-1)})"
        return f"({ex('string', depth-1)} + {ex('string', depth-1)})"

    for _ in range(nstmts):
        t = r.choice(["int", "float", "string"])
        name = fresh()
        lines.append(f"    {name} := {ex(t, 3)}")
        V[t].append(name)

    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 10
    sys.stdout.write(gen(seed, n))
