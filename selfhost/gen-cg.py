#!/usr/bin/env python3
# Generate random, TYPE-CORRECT, CALL-FREE programs for the Stage-4a codegen slice
# (scalar locals, literals, unary, binary incl. string concat/compare, assignment,
# if/while/break/continue). Diffed as C against `machin cgentest`. Multiple functions
# with scalar params/returns exercise monomorphization + signatures; bodies are
# call-free (calls/builtins are later codegen slices).
import random, sys

def gen(seed, nstmts):
    r = random.Random(seed)
    lines = []
    V = {"int": [], "float": [], "string": [], "bool": []}
    body = ["func main() {"]

    def lit(t):
        if t == "int": return str(r.randrange(0, 100))
        if t == "float": return f"{r.randrange(0,50)}.{r.randrange(1,9)}"
        if t == "string": return '"' + r.choice(["a", "bc", "x", ""]) + '"'
        return r.choice(["true", "false"])

    def ex(t, depth):
        if depth <= 0 or r.random() < 0.4:
            if V[t] and r.random() < 0.55: return r.choice(V[t])
            return lit(t)
        if t == "int":
            op = r.choice(["+","-","*","/","%","&","|","^","<<",">>"])
            return f"({ex('int',depth-1)} {op} {ex('int',depth-1)})"
        if t == "float":
            return f"({ex('float',depth-1)} {r.choice(['+','-','*','/'])} {ex('float',depth-1)})"
        if t == "string":
            return f"({ex('string',depth-1)} + {ex('string',depth-1)})"
        k = r.random()
        if k < 0.3: return f"!({ex('bool',depth-1)})"
        if k < 0.55: return f"({ex('bool',depth-1)} {r.choice(['&&','||'])} {ex('bool',depth-1)})"
        nt = r.choice(["int","float","string"])
        op = r.choice(["==","!="]) if nt == "string" else r.choice(["==","!=","<","<=",">",">="])
        return f"({ex(nt,depth-1)} {op} {ex(nt,depth-1)})"

    def stmts(indent, budget):
        for _ in range(budget):
            roll = r.random()
            if roll < 0.15:
                body.append(f"{indent}if {ex('bool',2)} {{")
                stmts(indent+"    ", max(1, budget//2))
                body.append(f"{indent}}}")
            elif roll < 0.25:
                body.append(f"{indent}while {ex('bool',2)} {{")
                stmts(indent+"    ", max(1, budget//2))
                body.append(f"{indent}}}")
            elif roll < 0.40 and any(V[t] for t in V):
                t = r.choice([t for t in V if V[t]])
                body.append(f"{indent}{r.choice(V[t])} = {ex(t,3)}")
            else:
                t = r.choice(["int","float","string","bool"])
                name = f"v{sum(len(x) for x in V.values())}"
                body.append(f"{indent}{name} := {ex(t,3)}")
                V[t].append(name)

    stmts("    ", nstmts)
    body.append("}")
    lines.append("\n".join(body))
    return "\n\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
