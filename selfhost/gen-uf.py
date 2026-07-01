#!/usr/bin/env python3
# Generate randomized union-find op scripts (deterministic per seed) to stress the
# engine: a mix of slot constructors then a run of unions. Prints one script to stdout.
import random, sys

KINDS0 = ["var", "num", "int", "float", "bool", "string", "void", "bytes"]

def gen(seed, nslots, nunions):
    r = random.Random(seed)
    lines = []
    n = 0
    def emit(line):
        nonlocal n
        lines.append(line)
    for _ in range(nslots):
        # bias toward composite kinds once a few base slots exist
        choice = r.random()
        if n >= 2 and choice < 0.18:
            e = r.randrange(n); emit(f"slice {e}")
        elif n >= 2 and choice < 0.30:
            e = r.randrange(n); emit(f"chan {e}")
        elif n >= 2 and choice < 0.45:
            k = r.randrange(n); v = r.randrange(n); emit(f"map {k} {v}")
        elif n >= 3 and choice < 0.62:
            # func with 0-3 params
            np = r.randrange(0, 4)
            ps = ",".join(str(r.randrange(n)) for _ in range(np)) if np else "-"
            ret = r.randrange(n) if r.random() < 0.7 else "-"
            emit(f"func {ps}|{ret}")
        elif choice < 0.74:
            name = r.choice(["Foo", "Bar", "Baz", "Node", "Tok"])
            emit(f"struct {name}")
        else:
            emit(r.choice(KINDS0))
        n += 1
    for _ in range(nunions):
        a = r.randrange(n); b = r.randrange(n)
        emit(f"union {a} {b}")
    emit("dump")
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    seed = int(sys.argv[1]) if len(sys.argv) > 1 else 0
    nslots = int(sys.argv[2]) if len(sys.argv) > 2 else 40
    nunions = int(sys.argv[3]) if len(sys.argv) > 3 else 30
    sys.stdout.write(gen(seed, nslots, nunions))
