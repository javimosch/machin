# MFL examples

Every program here is a **`.mfl`** file: the actual MFL language — **base64, one
function per line, blank line between functions**. There is no human-readable
source file; the `.mfl` *is* the source. That's the machine-first point: the
human states intent, the machine reads and writes the code.

```sh
machin run examples/complex/primes.mfl      # execute the program
machin decode examples/complex/primes.mfl   # inspection escape-hatch only
./examples/run.sh                            # run all programs
```

`decode` exists so a human *can* peek when they want to — but reading MFL is the
machine's job, not a required step in the workflow.

## basic/

| program | shows |
|---------|-------|
| `hello`        | minimal program |
| `arithmetic`   | operator precedence, int vs float division |
| `variables`    | `:=`, `=`, `var`, `len`, boolean logic |
| `conditionals` | `if / else if / else` |
| `loops`        | `while`, running sum, countdown |
| `functions`    | parameters, return values, composition |
| `temperature`  | float formulas, authored directly as MFL |

## complex/

| program | shows |
|---------|-------|
| `primes`          | trial-division primality, list primes ≤ N |
| `gcd_lcm`         | Euclidean gcd, derived lcm |
| `collatz`         | 3n+1 sequence length; busiest start < 100 |
| `ackermann`       | deep non-primitive recursion |
| `fast_power`      | exponentiation by squaring |
| `isqrt`           | Newton's-method integer sqrt + perfect-square test |
| `to_binary`       | recursive base conversion via string building |
| `pi_leibniz`      | float arithmetic, Leibniz series for π |
| `perfect_numbers` | proper-divisor sums |
