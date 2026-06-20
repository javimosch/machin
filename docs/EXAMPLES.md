# Example programs

Every program under `examples/` is a [base64-encoded](../README.md) `.mfl`
file — the encoded form is the source of truth. Run any of them with:

```sh
machin run examples/complex/collatz.mfl
```

or run the whole suite (long-running servers are skipped automatically):

```sh
./examples/run.sh
```

## Basic (`examples/basic/`)

Small programs that introduce one feature at a time.

| File | What it shows |
|------|---------------|
| `hello.mfl` | `println` and string literals |
| `variables.mfl` | `:=` declaration and reassignment |
| `arithmetic.mfl` | integer/float operators and precedence |
| `conditionals.mfl` | `if` / `else` branching |
| `loops.mfl` | Go-style `for` and `while`-form loops |
| `functions.mfl` | function declaration, parameters, return |
| `temperature.mfl` | a worked C↔F conversion using floats |

## Complex (`examples/complex/`)

Larger algorithms that combine recursion, loops, slices, and string building.

| File | What it computes |
|------|------------------|
| `ackermann.mfl` | the Ackermann function (deep recursion) |
| `collatz.mfl` | Collatz stopping times; longest chain under 100 |
| `fast_power.mfl` | exponentiation by squaring |
| `gcd_lcm.mfl` | Euclid's GCD and LCM |
| `isqrt.mfl` | integer square root |
| `perfect_numbers.mfl` | perfect-number search |
| `pi_leibniz.mfl` | π via the Leibniz series (float accumulation) |
| `primes.mfl` | prime generation by trial division |
| `to_binary.mfl` | decimal→binary via recursive string building |
| `slices.mfl` | `[]int` literals, `append`, indexing, `len` |
| `goroutines.mfl` | `go` statements and `sleep` |
| `http_server.mfl` | a concurrent HTTP server (long-running) |

## Benchmarks (`examples/bench/`)

| File | What it computes |
|------|------------------|
| `fib.mfl` | naive recursive Fibonacci — the workload behind the README's performance table |

## Adding an example

1. Write the readable source, then encode it:
   ```sh
   machin encode mysource.txt > examples/complex/mine.mfl
   ```
2. Verify it runs: `machin run examples/complex/mine.mfl`
3. Add a row to the table above.
