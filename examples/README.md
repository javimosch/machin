# MFL examples

Two files per example:

- **`*.mfl`** — the actual MFL program: **base64, one function per line, blank
  line between functions**. This is what `machin run` executes.
- **`*.mfs`** — the human-readable *projection* of the same program (Go-like
  text). Used only for authoring; `machin encode` turns it into the `.mfl`.

```sh
machin run examples/complex/primes.mfl    # run the real (base64) program
machin decode examples/complex/primes.mfl # view it as readable source
machin encode examples/complex/primes.mfs # (re)generate the .mfl
```

Regenerate and run everything:

```sh
./examples/build.sh
```

## basic/

| example | shows |
|---------|-------|
| `hello`        | minimal program |
| `arithmetic`   | operator precedence, int vs float division |
| `variables`    | `:=`, `=`, `var`, `len`, boolean logic |
| `conditionals` | `if / else if / else` |
| `loops`        | `while`, running sum, countdown |
| `functions`    | parameters, return values, composition |

## complex/

| example | shows |
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
