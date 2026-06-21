# The MFL Language Specification

Version 0.3.0

MFL (Machine-First Language) is a statically-typed, Go-flavored backend language
whose canonical on-disk form is **base64**. It is compiled to native code through
C. This document specifies the language as implemented by the `machin` toolchain.

> This is a reference specification. For a gentler tour see
> [`docs/LANGUAGE.md`](docs/LANGUAGE.md); for runnable programs see
> [`examples/`](examples/).

---

## 1. Overview

- **Machine-first.** A program *is* base64: one function (or type) declaration
  per line, a blank line between declarations. The base64 is the source of
  truth; the decoded text below is a projection humans never need to author by
  hand. The human states intent; the machine reads and writes the code.
- **Statically typed, no annotations.** Every value has a type known at compile
  time, inferred by unification. There is no type syntax except struct field
  types and the element types in `make`/composite literals.
- **Compiled, native.** Programs are translated to C99 and compiled with
  `cc -O2`. Values are unboxed; performance matches hand-written C.

---

## 2. Program structure and encoding

A `.mfl` file is a sequence of base64-encoded **declarations**, one per
non-blank line:

```
<base64 of declaration 1>

<base64 of declaration 2>
```

Each line base64-decodes to exactly one top-level declaration: a **function**
(`func ...`) or a **struct type** (`type ...`). The decoded form is the grammar
described in the rest of this document. Whitespace within a decoded declaration
is insignificant except as a token separator.

A program must define a function named `main` with no parameters and no return
value; it is the entry point.

The toolchain commands:

| Command | Effect |
|---------|--------|
| `machin run <file.mfl>` | compile to native and execute |
| `machin build <file.mfl> [-o out]` | compile to a native binary |
| `machin build <file.mfl> --emit-c` | print the generated C |
| `machin encode <src>` | encode loose declaration text into canonical `.mfl` |

---

## 3. Lexical elements

The decoded text of a declaration is tokenized as follows.

- **Comments.** `// ...` to end of line. (Stripped during encoding; not present
  in the canonical single-line form.)
- **Identifiers.** `[A-Za-z_][A-Za-z0-9_]*`. `_` is the blank identifier.
- **Integer literals.** decimal digits, e.g. `0`, `42`.
- **Float literals.** digits with a `.`, e.g. `3.14`, `0.5`.
- **String literals.** `"..."` with escapes `\n \t \r \" \\`.
- **Keywords.** `func return if else while for range true false nil var go type
  struct chan make map`.
- **Operators and punctuation.** `+ - * / % == != < <= > >= && || ! = := <- .
  : , ; ( ) { } [ ]`.

---

## 4. Types

| Type | Description | C representation |
|------|-------------|------------------|
| `int` | 64-bit signed integer | `int64_t` |
| `float` | IEEE-754 double | `double` |
| `bool` | boolean | `int` (0/1) |
| `string` | immutable text; zero value `""` | `char*` |
| `[]T` | slice of `T` (header + backing array) | `mfl_slice` |
| `map[K]V` | hash map, `K` is `int` or `string` | `mfl_map*` |
| `chan T` | channel of `T` | `mfl_chan*` |
| `func(...) R` | function value / closure | `mfl_closure` |
| named `struct` | record of typed fields | generated `struct` |

Types are **inferred**: a binding's type is determined by how it is used.
Mixing incompatible types is a compile-time error. An integer literal has a
flexible numeric type that resolves to `int` unless unified with a `float`.

### 4.1 Zero values

`int`/`bool` → `0`, `float` → `0.0`, `string` → `""`, slice/map/chan/func → an
empty/null value. A map read of an absent key yields the value type's zero value.

---

## 5. Declarations

### 5.1 Struct types

```go
type User struct {
    name   string
    age    int
    tags   []string
}
```

A field type is `int`, `float`, `bool`, `string`, `[]T`, `map[K]V`, `func` (a
function value), or another struct name. Structs have **value semantics**:
assigning or passing one copies it.

The `func` type denotes a function value whose signature is inferred from use —
it lets closures be stored in slices, maps (`make(map[string]func)`), and struct
fields, which is how a router keeps a table of handlers.

### 5.2 Functions

```go
func add(a, b) { return a + b }
```

Parameters are untyped in the syntax; their types are inferred. A function may
return zero, one, or several values (§9). Functions are **implicitly generic**
(§11).

The last parameter may be **variadic**, written `name...`. It collects the
trailing call arguments into a slice:

```go
func sum(nums...) {            // nums is a slice
    t := 0
    for _, n := range nums { t = t + n }
    return t
}

sum(1, 2, 3)                   // collect: nums = []int{1, 2, 3}
sum(xs...)                     // spread: nums = xs
```

### 5.3 Variables

```go
x := expr      // declare, type inferred from expr
var x = expr   // same
x = expr       // assign (x must exist; types must match)
```

Variables have **function scope** (a name denotes one binding, of one type,
throughout the function body).

---

## 6. Expressions

- **Literals:** integer, float, string, `true`, `false`.
- **Composite literals:** `[]T{...}`, `T{f: v, ...}` / `T{v, ...}`.
- **Construction:** `make(map[K]V)`, `make(chan T)`, `func(params){...}`.
- **Operators**, by increasing precedence:
  `||` · `&&` · `== != < <= > >=` · `+ -` · `* / %` · unary `- ! <-`.
- **Arithmetic** `+ - * / %`: numeric operands. `/` of two `int` is integer
  division; `%` is `int`-only. Mixing an `int` with a `float` promotes to
  `float`.
- **`+`** on two `string`s concatenates.
- **Comparisons** yield `bool`; `&&`/`||` short-circuit.
- **Indexing** `x[i]`: slice (by `int`) or map (by key); also map/slice
  assignment `x[i] = v`.
- **Field access** `x.f` and assignment `x.f = v`.
- **Calls** `f(args)`; a function value may also be called: `g()(x)`, `fs[i](x)`.
- **Receive** `<-ch`.

---

## 7. Statements

- Expression statement, assignment (`=`, `:=`, `var`).
- Multiple/parallel assignment: `a, b := f()` / `a, b = e1, e2` (§9).
- `if cond { ... } else if cond { ... } else { ... }` — conditions are `bool`.
- Loops:
  - `while cond { ... }`
  - `for cond { ... }` and `for { ... }` (infinite)
  - `for k, v := range x { ... }` over a slice (index, element), map (key,
    value), or string (index, 1-char). Either variable may be `_`.
- `return`, `return e`, `return e1, e2`.
- `x[i] = v`, `x.f = v`.
- `ch <- v` (send).
- `go f(args)` (§10).

---

## 8. Builtins

| Builtin | Signature | Purpose |
|---------|-----------|---------|
| `print`, `println` | `(...) -> ` | write arguments (no / trailing newline) |
| `len` | `(string\|slice\|map) -> int` | length |
| `str` | `(int\|float) -> string` | format a number |
| `int` | `(number) -> int` | truncate to int |
| `append` | `([]T, T) -> []T` | grow a slice |
| `has`, `delete` | `(map, K) -> bool` / `-> ` | membership / removal |
| `keys` | `(map[K]V) -> []K` | a map's keys |
| `json` | `(any) -> string` | serialize to JSON |
| `parse` | `(string, T{}) -> T` | parse JSON into `T`'s type (witness) |
| `http_body` | `(string) -> string` | body of an HTTP message |
| `substr` | `(string, int, int) -> string` | substring |
| `index` | `(string, string) -> int` | first index, or `-1` |
| `contains`, `has_prefix`, `has_suffix` | `(string, string) -> bool` | text tests |
| `charat` | `(string, int) -> string` | 1-character string |
| `to_upper`, `to_lower`, `trim` | `(string) -> string` | case / whitespace |
| `replace` | `(string, string, string) -> string` | replace all |
| `split` | `(string, string) -> []string` | split |
| `join` | `([]string, string) -> string` | join |
| `sleep` | `(int) -> ` | pause (milliseconds) |
| `listen`, `accept` | `(int) -> int` | open / accept on a TCP socket |
| `read`, `write` | `(int[, string]) -> string\|int` | socket I/O |
| `close` | `(int) -> ` | close a socket |

---

## 9. Multiple return values

A function may return several values; a call destructures them:

```go
func divmod(a, b) { return a / b, a % b }
q, r := divmod(17, 5)
v, ok := lookup(m, k)     // (value, ok) idiom
a, b = b, a              // parallel assignment, RHS evaluated first
```

A multi-value call may appear only as the sole right-hand side of a
multi-assignment. `_` discards a value. A function returning ≥2 values compiles
to a generated result struct.

**Named returns.** A function may name its return values in the signature; they
become zero-initialized locals, and a bare `return` (or falling off the end)
yields their current values:

```go
func divmod(a, b) (q, r) {
    q = a / b
    r = a % b
    return            // returns q, r
}
```

---

## 10. Concurrency

- `go f(args)` runs a function call in a new goroutine (a POSIX thread).
- **Channels:** `make(chan T)` creates a channel; `ch <- v` sends; `<-ch`
  receives, blocking until a value is available. The element type is inferred.
- `sleep(ms)` suspends the current goroutine.

Channels are an unbounded FIFO: sends do not block; a receive blocks until a
value arrives.

---

## 11. Functions, closures, and generics

### 11.1 Function values and closures

A `func(params) { ... }` literal is a value that can be stored, passed, and
returned. It **captures** free variables from the enclosing scope **by
reference** (as in Go): a captured variable lives in a shared heap cell, so
assignments made through the closure are visible to the enclosing scope and to
any other closure over the same variable, and vice versa. This makes the
mutable-state idiom work — e.g. a `counter()` that returns a closure
incrementing a captured local on each call. Function values hold a single return
value (or none). Compilation is by closure conversion (lambda-lifting): a
literal becomes a top-level function plus an environment of pointers to the
captured cells.

### 11.2 Generics

Functions are **implicitly generic**: because parameter types are inferred, a
function imposes no constraint beyond its use, so the same source function works
at many types. Each call is compiled by **monomorphization** — the compiler
emits one specialized native function per concrete call-site signature
(deduplicated). There is no boxing. Recursion is monomorphic (one concrete type
per instantiation).

```go
func id(x) { return x }
id(42); id("hi"); id(3.14)   // → three native functions
```

---

## 12. Execution and memory

- A program runs by calling `main`. The process exits 0 on normal completion.
- **Memory** is managed by a per-goroutine **arena**: value buffers (strings,
  slice backings, closure environments) are allocated from the running
  goroutine's arena and reclaimed in bulk when that goroutine finishes. The main
  goroutine's arena lives for the whole program. This bounds the memory of a
  long-running concurrent server — each request handler runs in its own
  goroutine and frees everything it allocated on return.
  - *Caveat:* a value allocated in one goroutine and shared with another (e.g.
    a heap pointer sent over a channel, or stored in a map outliving the
    sender) may be reclaimed while still referenced. Pass such values by copy or
    keep them in the receiver's scope.
- By default, integer overflow wraps (two's complement) and division by zero /
  out-of-bounds slice access are undefined (they follow the generated C).
- Building with **`--safe`** inserts runtime checks: a slice index out of range,
  integer division/modulo by zero, or integer `+`/`-`/`*` overflow prints a
  `panic:` message to stderr and exits non-zero. `--safe` is opt-in; the default
  build has zero check overhead.

---

## 13. Compilation model

```
.mfl ──base64 decode──▶ parse ──▶ lambda-lift ──▶ infer + monomorphize ──▶ emit C ──▶ cc -O2 ──▶ native binary
```

- **Inference** is unification over a union-find; deferred resolution handles
  `x[i]`, `x.f`, and `range` once the base type is known.
- **Monomorphization** instantiates the reachable call graph from `main`,
  specializing each function per concrete type and deduplicating identical
  instances.
- **Codegen** emits one C function per instance with unboxed value types, plus a
  small C runtime (slices, maps, channels, closures, sockets, JSON, strings).

---

## 14. Grammar (decoded form, informal)

```
Program     = { Decl } .
Decl        = FuncDecl | TypeDecl .
TypeDecl    = "type" ident "struct" "{" { ident TypeName } "}" .
FuncDecl    = "func" ident "(" [ identList [ "..." ] ] ")" [ "(" identList ")" ] Block .
TypeName    = "int" | "float" | "bool" | "string" | ident
            | "[]" TypeName | "map" "[" TypeName "]" TypeName | "chan" TypeName .
Block       = "{" { Stmt } "}" .
Stmt        = Decl? | Assign | If | Loop | Return | Send | Go | ExprStmt .
Assign      = identList ( ":=" | "=" ) exprList .
If          = "if" Expr Block [ "else" ( If | Block ) ] .
Loop        = ( "while" | "for" ) [ Expr ] Block
            | "for" ident [ "," ident ] ":=" "range" Expr Block .
Return      = "return" [ exprList ] .
Send        = Expr "<-" Expr .
Go          = "go" Call .
Expr        = ... operators, calls, indexing, field access, literals,
              FuncLit, make, "<-" Expr ... .
FuncLit     = "func" "(" [ identList ] ")" Block .
```

---

## 15. Status and non-goals

Implemented: the entire surface above, including arena memory management (§12),
named return values (§9), variadic parameters (§5.2), by-reference closure
capture (§11.1), and opt-in bounds/overflow checks (`--safe`). Not yet
implemented: polymorphic recursion and a tracing GC across goroutines. These are
refinements, not core gaps.
