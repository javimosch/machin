# machin CLI reference

`machin` is the MFL toolchain. A program is a `.mfl` file: base64, one function
per line, a blank line between functions. The base64 **is** the program — there
is no human-readable source of truth and no decode step in the workflow.

```
machin <command> [args]
```

## Commands

### `machin run <file.mfl>`

Compile the program to a native binary in a temp location and execute it
immediately. The child process inherits stdin/stdout/stderr, and `machin`
forwards the child's exit code.

```bash
machin run examples/demo.mfl
```

Use this for the edit/run loop. Nothing is left on disk.

### `machin build <file.mfl> [-o <out>]`

Compile the program to a standalone native binary.

- `-o <out>` — name the output binary. Defaults to the source filename with its
  extension stripped (e.g. `primes.mfl` → `primes`).

```bash
machin build examples/complex/primes.mfl -o primes
./primes
```

### `machin build <file.mfl> --emit-c`

Print the C that the compiler emits to stdout and stop — no binary is produced.
Handy for inspecting codegen or feeding the C to another toolchain.

```bash
machin build examples/demo.mfl --emit-c
```

### `machin encode <src>`

Mint canonical MFL from loose Go-like text. Each function in `<src>` is
normalized to one line, type-checked as a whole program, and emitted as base64
(one function per line, blank line between). This is a machine convenience for
*producing* `.mfl`, not a human authoring path.

```bash
# author.txt holds ordinary Go-like functions
machin encode author.txt > program.mfl
```

`encode` runs the full type checker before emitting, so a program that fails
inference is rejected here rather than at build time. `//` line comments and
inter-function whitespace in the input are stripped; whitespace inside string
literals is preserved.

### `machin help` (`-h`, `--help`)

Print usage.

## Exit codes

| Code | Meaning                                                        |
|------|---------------------------------------------------------------|
| 0    | Success.                                                       |
| 1    | Compilation error (parse, type, or `cc` failure).             |
| 2    | Usage error (missing/unknown command or bad arguments).       |
| *n*  | For `run`, the exit code of the compiled program itself.       |

## Requirements

`machin` shells out to a C compiler (`cc`) with `-O2` to produce native code, so
a working C toolchain must be on `PATH`.
