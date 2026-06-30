#!/usr/bin/env python3
# Generate random, TYPE-CORRECT main-only programs in sub-slice-2's supported grammar
# (the `encode` step only accepts well-typed programs). Each variable has a stable
# type; we don't mix int/float (the flex case is covered by hand-written tests). The
# output is diffed against the Go checker to fuzz gen_expr/gen_binary/gen_stmt/solve.
import random, sys

def gen(seed, nstmts):
    r = random.Random(seed)
    # vars by type
    V = {"int": [], "float": [], "string": [], "bool": []}
    lines = ["func main() {"]

    def lit(t):
        if t == "int": return str(r.randrange(0, 100))
        if t == "float": return f"{r.randrange(0,50)}.{r.randrange(1,9)}"
        if t == "string": return '"' + r.choice(["a", "bc", "hi", "x", ""]) + '"'
        return r.choice(["true", "false"])

    def ex(t, depth):
        # produce an expression of type t
        if depth <= 0 or r.random() < 0.35:
            if V[t] and r.random() < 0.6:
                return r.choice(V[t])
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
        # bool
        k = r.random()
        if k < 0.3:
            return f"!({ex('bool', depth-1)})"
        if k < 0.55:
            return f"({ex('bool', depth-1)} {r.choice(['&&','||'])} {ex('bool', depth-1)})"
        nt = r.choice(["int", "float", "string"])
        if nt == "string":
            op = r.choice(["==", "!="])
        else:
            op = r.choice(["==", "!=", "<", "<=", ">", ">="])
        return f"({ex(nt, depth-1)} {op} {ex(nt, depth-1)})"

    def stmts(indent, budget):
        for _ in range(budget):
            roll = r.random()
            if roll < 0.14:
                lines.append(f"{indent}if {ex('bool', 2)} {{")
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.22:
                lines.append(f"{indent}while {ex('bool', 2)} {{")
                stmts(indent + "    ", max(1, budget // 2))
                lines.append(f"{indent}}}")
            elif roll < 0.34:
                # reassign an existing var with same-type expr
                t = r.choice([t for t in ("int","float","string","bool") if V[t]] or ["int"])
                if V[t]:
                    lines.append(f"{indent}{r.choice(V[t])} = {ex(t, 3)}")
                    continue
                # fall through to declaration
                roll = 1.0
            # declaration: scalar or slice literal
            if r.random() < 0.18:
                et = r.choice(["int", "float", "string", "bool"])
                n = r.randrange(0, 4)
                elems = ", ".join(ex(et, 1) for _ in range(n))
                name = f"v{sum(len(x) for x in V.values())}"
                lines.append(f"{indent}{name} := []{et}{{{elems}}}")
                V.setdefault("slice", []).append(name)
            else:
                t = r.choice(["int", "float", "string", "bool"])
                name = f"v{sum(len(x) for x in V.values() if x is not None)}"
                lines.append(f"{indent}{name} := {ex(t, 3)}")
                V[t].append(name)

    stmts("    ", nstmts)
    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
