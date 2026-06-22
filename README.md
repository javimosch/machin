<p align="center">
  <img src="https://img.shields.io/badge/version-0.9.0-blue" alt="Version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/go-1.22-00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/backend-C%20%E2%86%92%20native-orange" alt="Native">
</p>

# machin ⎯ Machine-First Language (MFL)

A Go-flavored **backend language shaped for agents**: terse, type-inferred, one canonical declaration per line. Compiles to native code through C — C/Rust/Zig-class speed, a single static binary, no runtime.

[Spec](SPEC.md) · [Language tour](docs/LANGUAGE.md) · [Agent guide](AGENTS.md) · [Ecosystem → **awesome-machin**](https://github.com/javimosch/awesome-machin) · [Landing](https://javimosch.github.io/machin/) · [Releases](https://github.com/javimosch/machin/releases)

> This README is deliberately terse — machin is machine-first, and so are its docs. Depth lives in [`SPEC.md`](SPEC.md) and [`AGENTS.md`](AGENTS.md).

## The form

`.mfl` is plain canonical text: one normalized declaration per line, whitespace tightened. Greppable, diffable, no type annotations.

```
func fib(n){if n<2{return n}return fib(n-1)+fib(n-2)}

func main(){println(fib(10))}
```

Why not base64 (the old design)? Measured with [`tools/tokcost.py`](tools/tokcost.py), base64 costs an agent ~2.5× the output tokens to write/edit. A dense `machin pack` form still exists for distribution; `machin run` reads either.

## Use

```bash
make build                               # → bin/machin   (needs Go 1.22 + a C compiler)
bin/machin run    examples/demo.mfl      # compile to native + execute
bin/machin build  app.mfl -o app         # standalone native binary
bin/machin build  app.mfl --emit-c       # print the generated C
bin/machin build  app.mfl --safe         # + bounds / div-zero / overflow checks
bin/machin encode a.src b.src > app.mfl  # mint canonical .mfl from loose Go-like text
bin/machin pack   app.mfl                # dense base64 form (distribution)
```

## Capabilities

- **Types** (all inferred, unboxed): `int` `float` `bool` `string`, slices `[]T`, maps `map[K]V`, structs, `func` values
- **Flow**: `if`/`for`/`while`/`range`, `break`/`continue`, multiple & named returns, comma-ok, variadics, closures (by-reference), implicit generics (monomorphized)
- **Concurrency**: goroutines (`go`), channels; per-goroutine arena GC + scoped `arena{}`; `--safe` checks
- **Networking**: `dial` (client) + `listen`/`accept`/`read`/`write`/`close` (server); native TLS `https_get`/`https_post` (OpenSSL, linked only when used)
- **I/O & data**: `read_file`/`write_file`/`list_dir`/`mkdir`, `input`, `json`/`parse`, `args`/`env`/`now`/`now_ms`/`parse_int`, string ops
- **C FFI**: `extern` blocks — scalars, by-value structs (`cstruct`), opaque `ptr` handles, multi-`link` (drove a real raylib **GUI**)
- **machweb**: a web framework written in MFL ([`framework/`](framework/))

Full surface and grammar: [`SPEC.md`](SPEC.md). Runnable programs: [`examples/`](examples/).

## Ecosystem

Things built with machin — the curated list is [**awesome-machin**](https://github.com/javimosch/awesome-machin):

- [boilerplate-cli-ui-machin](https://github.com/javimosch/boilerplate-cli-ui-machin) — single-binary CLI + embedded React web UI + daemon (via FFI)
- [machin-healthcheck](https://github.com/javimosch/machin-healthcheck) — concurrent HTTP status/latency checker
- [machin-ssg](https://github.com/javimosch/machin-ssg) — static-site generator (markdown → HTML)

Built something? Add it to [awesome-machin](https://github.com/javimosch/awesome-machin).

## Performance

`fib(40)` (~331M calls): **MFL 0.20s** · C 0.19s · Rust 0.29s. Values are unboxed; no interpreter, no VM.

## License

MIT — <a href="https://www.linkedin.com/in/arancibiajav/">Javier Leandro Arancibia</a>
