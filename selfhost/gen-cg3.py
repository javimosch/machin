#!/usr/bin/env python3
# Stage-4c codegen fuzz: TYPE-CORRECT programs using scalar/string BUILTINS + println
# (no structs/slices/maps). Exercises the builtin C table, printCall, str/len/int/float/
# math dispatch, and seqExprs around builtin calls. Diffed as C against cgentest.
import random, sys

# builtins by (return type, arg types)
INT_B = [("parse_int", ["string"]), ("len", ["string"]), ("index", ["string","string"]),
         ("int", ["float"]), ("now", []), ("now_ms", [])]
FLOAT_B = [("sqrt", ["float"]), ("sin", ["float"]), ("cos", ["float"]), ("abs", ["float"]),
           ("float", ["int"]), ("parse_float", ["string"]), ("pow", ["float","float"]),
           ("floor", ["float"]), ("pi", [])]
STR_B = [("to_upper", ["string"]), ("to_lower", ["string"]), ("trim", ["string"]),
         ("substr", ["string","int","int"]), ("replace", ["string","string","string"]),
         ("charat", ["string","int"]), ("str", ["int"]), ("url_encode", ["string"]),
         ("base64_encode", ["string"]), ("sha256", ["string"]), ("env", ["string"])]
BOOL_B = [("contains", ["string","string"]), ("has_prefix", ["string","string"]),
          ("has_suffix", ["string","string"])]

def gen(seed, nstmts):
    r = random.Random(seed)
    V = {"int": [], "float": [], "string": [], "bool": []}
    lines = ["func main() {"]
    ctr = [0]
    def fresh():
        n = f"v{ctr[0]}"; ctr[0] += 1; return n
    def lit(t):
        if t == "int": return str(r.randrange(0, 100))
        if t == "float": return f"{r.randrange(0,50)}.{r.randrange(1,9)}"
        if t == "string": return '"' + r.choice(["a", "bc", "hi", "x"]) + '"'
        return r.choice(["true", "false"])
    def val(t, depth):
        if depth <= 0 or r.random() < 0.5:
            if V[t] and r.random() < 0.5: return r.choice(V[t])
            return lit(t)
        # a builtin call of return type t
        table = {"int": INT_B, "float": FLOAT_B, "string": STR_B, "bool": BOOL_B}[t]
        name, argts = r.choice(table)
        args = ", ".join(val(a, depth-1) for a in argts)
        return f"{name}({args})"
    for _ in range(nstmts):
        roll = r.random()
        if roll < 0.2:
            # println with 1-3 scalar args
            k = r.randrange(1, 4)
            args = ", ".join(val(r.choice(["int","float","string","bool"]), 2) for _ in range(k))
            lines.append(f"    println({args})")
        else:
            t = r.choice(["int", "float", "string", "bool"])
            name = fresh()
            lines.append(f"    {name} := {val(t, 3)}")
            V[t].append(name)
    lines.append("}")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 12
    sys.stdout.write(gen(seed, n))
