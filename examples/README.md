# MFL examples

Every program here is a **`.mfl`** file: the actual MFL language — **base64, one
function per line, blank line between functions**. There is no human-readable
source file; the `.mfl` *is* the source. That's the machine-first point: the
human states intent, the machine reads and writes the code. Each program is
compiled to native code (through C) and executed.

```sh
machin run examples/complex/primes.mfl          # compile to native + run
machin build examples/complex/primes.mfl -o p   # produce a native binary
machin build examples/complex/primes.mfl --emit-c   # see the generated C
./examples/run.sh                                # run all programs
```

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
| `sieve`           | Sieve of Eratosthenes — flag slice, in-place marking |
| `gcd_lcm`         | Euclidean gcd, derived lcm |
| `collatz`         | 3n+1 sequence length; busiest start < 100 |
| `ackermann`       | deep non-primitive recursion |
| `fast_power`      | exponentiation by squaring |
| `isqrt`           | Newton's-method integer sqrt + perfect-square test |
| `to_binary`       | recursive base conversion via string building |
| `pi_leibniz`      | float arithmetic, Leibniz series for π |
| `perfect_numbers` | proper-divisor sums |
| `fib_table`       | iterative Fibonacci built bottom-up into a slice (DP) |
| `slices`          | slice literals, `append`, indexing, `len`, in-place reverse |
| `goroutines`      | `go` spawns concurrent workers; `sleep` waits |
| `http_server`     | concurrent TCP/HTTP server — `go handle(conn)` per request |

`http_server` loops forever, so it's skipped by `run.sh`. Run it directly:

```sh
machin run examples/complex/http_server.mfl
# in another shell:
curl -i http://localhost:48080/
```

## bench/

| program | shows |
|---------|-------|
| `fib`   | `fib(40)` — native MFL runs neck-and-neck with hand-written C |
