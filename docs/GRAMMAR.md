# MFL Grammar Reference

This document specifies the **decoded** form of MFL — the readable text that each
base64 line of an `.mfl` file expands to. On disk a program is base64 (one
function per line, a blank line between functions); the grammar below describes
what a single decoded line must contain: exactly one function declaration.

All facts here are derived from the compiler itself (`lexer.go`, `parser.go`,
`codegen.go`) and verified against the native compile-and-run path.

## File layout

```
program     ::= func_line ( blank_line func_line )*
func_line   ::= <base64 of exactly one function declaration>
blank_line  ::= "\n"
```

A decoded `func_line` is a single `FuncDecl`. There are no top-level statements,
imports, or global variables — execution starts at `main`.

## EBNF (decoded source)

```ebnf
FuncDecl   ::= "func" ident "(" params? ")" Block
params     ::= ident ( "," ident )*
Block      ::= "{" Stmt* "}"

Stmt       ::= ShortDecl
             | VarDecl
             | Assign
             | If
             | While
             | For
             | Return
             | Go
             | ExprStmt

ShortDecl  ::= ident ":=" Expr
VarDecl    ::= "var" ident ( "=" Expr )?
Assign     ::= LValue "=" Expr
LValue     ::= ident | ident "[" Expr "]"
If         ::= "if" Expr Block ( "else" ( If | Block ) )?
While      ::= "while" Expr Block
For        ::= "for" SimpleStmt? ";" Expr? ";" SimpleStmt? Block
Return     ::= "return" Expr?
Go         ::= "go" CallExpr
ExprStmt   ::= Expr

Expr       ::= Binary
Binary     ::= Unary ( binop Unary )*
Unary      ::= ( "-" | "!" ) Unary | Postfix
Postfix    ::= Primary ( "[" Expr "]" )*
Primary    ::= int_lit | float_lit | string_lit
             | "true" | "false" | "nil"
             | ident
             | CallExpr
             | SliceLit
             | "(" Expr ")"
CallExpr   ::= ident "(" ( Expr ( "," Expr )* )? ")"
SliceLit   ::= "[" "]" type "{" ( Expr ( "," Expr )* )? "}"
```

Notes:

- Statements are **not** separated by semicolons (except inside the `for`
  header); the parser reads statements until the closing `}`.
- `:=` declares a new variable with an inferred type; `=` assigns to an
  existing variable or slice element.
- `for` uses the classic three-clause Go header: `for init; cond; post { ... }`.

## Reserved keywords

```
func   return   if   else   while   for
true   false    nil  var    go
```

## Operators and precedence

Precedence climbing, lowest binding first. Higher number = tighter binding.
All binary operators are left-associative.

| Precedence | Operators        | Meaning                          |
|-----------:|------------------|----------------------------------|
| 1          | `||`             | logical OR                       |
| 2          | `&&`             | logical AND                      |
| 3          | `==` `!=`        | equality / inequality            |
| 4          | `<` `<=` `>` `>=`| ordered comparison               |
| 5          | `+` `-`          | add / subtract                   |
| 6          | `*` `/` `%`      | multiply / divide / remainder    |

Unary operators (bind tighter than any binary operator):

| Operator | Meaning            |
|----------|--------------------|
| `-`      | numeric negation   |
| `!`      | logical NOT        |

Postfix `[ ]` (slice index) binds tighter than unary operators.

> **No bitwise operators.** MFL has no `^`, `&`, `|`, `<<`, or `>>`. The lexer
> rejects `^` (and bare `&`/`|`) outright. Compute bit tricks with `/`, `*`,
> and `%` instead.
>
> **`%` is integer-only**, matching C: applying it to floats is a type error.

## Types

Types are inferred by unification — there are no type annotations on variables
or parameters (the only place a type name appears is a slice literal's element
type, e.g. `[]int{...}`).

| Type      | Literals / source                  |
|-----------|------------------------------------|
| `int`     | `42`, `0`, `-7`                     |
| `float`   | `3.14`, `1.0`                       |
| `string`  | `"hello"`, with `\n \t \r \" \\`    |
| `bool`    | `true`, `false`                    |
| `[]T`     | `[]int{1, 2, 3}` (slices, growable)|

## Builtin functions

Signatures as implemented in `codegen.go`:

| Builtin                      | Purpose                                          |
|------------------------------|--------------------------------------------------|
| `print(args...)`             | write arguments to stdout (no trailing newline)  |
| `println(args...)`           | write arguments space-separated + newline        |
| `len(x)`                     | length of a slice or string                      |
| `append(s, x)`               | return slice `s` with `x` appended               |
| `str(x)`                     | convert a value to its `string` form             |
| `int(x)`                     | convert a value to `int`                         |
| `sleep(ms)`                  | sleep for `ms` milliseconds                       |
| `listen(port)`               | open a listening TCP socket, return its fd        |
| `accept(fd)`                 | accept a connection, return the client fd         |
| `read(fd)`                   | read available bytes from `fd` as a string        |
| `write(fd, s)`               | write string `s` to `fd`                          |
| `close(fd)`                  | close file descriptor `fd`                        |

Concurrency: `go f(...)` runs a call on its own goroutine (POSIX thread in the
emitted C). See `examples/complex/goroutines.mfl` and
`examples/complex/http_server.mfl`.

## A complete example (decoded)

```go
func gcd(a, b) { while b != 0 { t := b b = a % b a = t } return a }

func main() { println("gcd(48, 36) =", gcd(48, 36)) }
```

Encoded, that is the two-line `.mfl` file the toolchain actually stores.
