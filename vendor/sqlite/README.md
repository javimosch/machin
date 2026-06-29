# Vendored SQLite amalgamation

`sqlite3.c.gz` / `sqlite3.h.gz` — the official SQLite **amalgamation**
(single-file build), **version 3.53.3**, from
<https://www.sqlite.org/2026/sqlite-amalgamation-3530300.zip>, gzipped.

SQLite is in the **public domain**.

These are embedded into the `machin` binary (`//go:embed`, gzipped ≈ 2.6 MB) and
compiled directly into a program by `machin build --static`, so a SQLite-using app
produces a fully static binary with no `libsqlite3` dependency — it runs
`FROM scratch`. Pair with `CC=musl-gcc` for a libc-free static binary.

To update: download a newer amalgamation zip from sqlite.org, gzip `sqlite3.c` and
`sqlite3.h` into this directory (`gzip -9 -c sqlite3.c > sqlite3.c.gz`), and bump
the version here.
