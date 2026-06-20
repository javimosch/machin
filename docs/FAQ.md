# MFL FAQ

### Why is the source base64 instead of plain text?

MFL is *machine-first*. The premise is that the entity authoring and consuming
the code is increasingly a machine, for which readable glyphs, indentation, and
naming conventions are overhead. A program is base64 — one function per line, a
blank line between functions — and that base64 **is** the program. There is no
separate human-readable source of truth and no decode step in the workflow.

### How do I write a program if it's base64?

You state intent and let the machine mint the `.mfl`. The `machin encode`
command lifts loose Go-like text into canonical MFL (see [CLI.md](CLI.md)). It
normalizes each function to one line, type-checks the whole program, and emits
base64. This is a convenience for *producing* MFL, not a human authoring path.

### Is it interpreted?

No. MFL compiles to C and hands the C to `cc -O2`, producing native machine
code. For scalar work it lands in the same performance class as hand-written C
(see the repo's benchmark report).

### Do I have to write type annotations?

No. Types are inferred by unification, so the surface stays minimal. A value's
type follows from how it is used — for example, a variable that starts as an
`int` unifies to `float` once it participates in a float expression.

### What language features are available?

The Go-like core: `func`, `if`/`else if`/`else`, `while` loops, `for` loops,
recursion, integer and float arithmetic, booleans, strings with `+`
concatenation, `:=` declaration and `=` assignment, slices, and goroutines.
`println` prints its arguments separated by spaces. See the example programs
under `examples/` for working code.

### What does `--emit-c` do?

`machin build <file> --emit-c` prints the C the compiler generates and stops,
without producing a binary. It is useful for inspecting codegen.

### What do I need installed?

A Go toolchain to build `machin` itself, and a C compiler (`cc`) on `PATH`,
since `machin` shells out to `cc -O2` to produce native binaries.

### Where do I start?

Run the demo and read the examples:

```bash
machin run examples/demo.mfl
ls examples/complex/
```
