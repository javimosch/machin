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
| `gcd_lcm`         | Euclidean gcd, derived lcm |
| `collatz`         | 3n+1 sequence length; busiest start < 100 |
| `ackermann`       | deep non-primitive recursion |
| `fast_power`      | exponentiation by squaring |
| `isqrt`           | Newton's-method integer sqrt + perfect-square test |
| `to_binary`       | recursive base conversion via string building |
| `pi_leibniz`      | float arithmetic, Leibniz series for π |
| `perfect_numbers` | proper-divisor sums |
| `sieve`           | Sieve of Eratosthenes over a slice |
| `slices`          | slice literals, `append`, indexing, `len`, in-place reverse |
| `maps`            | `map[string]int` word frequency + `map[int]string` lookup, `has`/`delete`/`keys` |
| `ranges`          | `for k, v := range` over slices, strings, and maps |
| `multi_return`    | multiple return values, comma-ok `(v, ok)`, `_`, parallel swap |
| `closures`        | capturing lambdas, higher-order functions, IIFE |
| `generics`        | one source function specialized at int / string / float (monomorphization) |
| `structs`         | `type` declaration, struct literals, field access, `[]struct` of records |
| `goroutines`      | `go` spawns concurrent workers; `sleep` waits |
| `channels`        | fan-in worker pool — goroutines communicate over a channel |
| `http_server`     | concurrent TCP/HTTP server — `go handle(conn)` per request |
| `json`            | `json()` serialization of scalars, slices, structs, maps |
| `json_api`        | JSON-over-HTTP API — each request returns JSON-serialized structs |
| `json_parse`      | `parse(s, T{})` — JSON into struct/slice/map/scalar, with round-trips |
| `json_echo_api`   | POST JSON → parse into a struct → echo it back as JSON |
| `strings`         | string ops (`split`/`join`/`substr`/`index`/`replace`/…) + request-line parsing |
| `router_api`      | HTTP router — dispatch by method+path, extract path params |

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
