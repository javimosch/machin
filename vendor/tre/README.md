# Vendored TRE

`tre-src.tar.gz` — **TRE 0.8.0** (master @ laurikari/tre), the tiny POSIX
regex library, from <https://github.com/laurikari/tre>. BSD-2-Clause
(see `LICENSE` inside the archive).

## Why

mingw-w64 has no `<regex.h>`/`regcomp`, so `--target windows` could not
link the `regex_*` builtins (#517). TRE is a drop-in POSIX ERE
implementation: `tre/regex.h` `#define`s `regcomp`/`regexec`/`regfree`/
`regerror` onto `tre_*`, and a top-level `regex.h` shim makes the
generated `#include <regex.h>` resolve to it.

## Build configuration

Compiled WITHOUT `TRE_MULTIBYTE`/`TRE_WCHAR`/`TRE_APPROX`/
`TRE_USE_ALLOCA` (see the generated `tre/tre-config.h`): byte-oriented
matching, the same semantics glibc `regexec` has in the C locale —
which is what the native build already gives UTF-8 text. `tre-match-approx.c`
is shipped but skipped at compile time (its symbols are only referenced
under `TRE_APPROX`).

## Vendored patches vs upstream

- `tre/tre-config.h` — hand-generated (upstream produces it via
  autotools); adds `TRE_REGEX_T_FIELD value`, which `configure.ac`
  normally defines.
- `lib/tre-internal.h` — `ALIGN()` casts through `size_t` instead of
  `long`; `long` is 32-bit on Windows (LLP64) and truncated heap
  pointers.
