#!/usr/bin/env python3
# Generate random, TYPE-CORRECT multi-function programs to fuzz sub-slice 3
# (user-function calls + monomorphization). A fixed set of GENERIC helpers (each
# works at int/float/string) is called from main at varying concrete types, so the
# same helper yields several monomorphic instances — stressing dedup + signature sort.
import random, sys

HELPERS = """\
func ident(x) (r) { r = x }
func dbl(x) (r) { r = x + x }
func combine(a, b) (r) { r = a + b }
func firstof(a, b) (r) { r = a }
func swap(a, b) (p, q) { p = b  q = a }
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

    def val(t):
        if V[t] and r.random() < 0.55:
            return r.choice(V[t])
        return lit(t)

    for _ in range(nstmts):
        t = r.choice(["int", "float", "string"])
        roll = r.random()
        name = fresh()
        if roll < 0.22:                       # ident(x)
            lines.append(f"    {name} := ident({val(t)})")
        elif roll < 0.44:                     # dbl(x)
            lines.append(f"    {name} := dbl({val(t)})")
        elif roll < 0.66:                     # combine(a,b)
            lines.append(f"    {name} := combine({val(t)}, {val(t)})")
        elif roll < 0.80:                     # firstof(a,b)  (mixed types allowed: r=a)
            t2 = r.choice(["int", "float", "string"])
            lines.append(f"    {name} := firstof({val(t)}, {val(t2)})")
        elif roll < 0.92:                     # swap(a,b) -> two returns
            t2 = r.choice(["int", "float", "string"])
            n2 = fresh()
            lines.append(f"    {name}, {n2} := swap({val(t)}, {val(t2)})")
            V[t2].append(n2)                  # p=b has type t2
            V[t].append(name)                 # q=a has type t
            continue
        else:                                 # plain literal
            lines.append(f"    {name} := {lit(t)}")
        V[t].append(name)

    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
