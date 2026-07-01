#!/usr/bin/env bash
# Self-host perf bench: MFL-compiled code vs the Go compiler, same algorithm.
# Decomposes the lexer gap into raw-compute vs allocation. See PERF.md.
set -uo pipefail
cd "$(dirname "$0")/.."
MACHIN="${MACHIN:-./bin/machin}"
go build -trimpath -o bin/machin . || exit 1

bench_one() { # <name> <mfl-bin> <go-bin> <args...>
  printf '%-28s MFL: ' "$1"; "$2" "${@:4}"
  printf '%-28s Go:  ' ''; "$3" "${@:4}"
}

# 1) raw integer loop
"$MACHIN" encode selfhost/bench/mb_int.src > /tmp/b_int.mfl 2>/dev/null \
  && "$MACHIN" build /tmp/b_int.mfl -o /tmp/b_int_mfl >/dev/null 2>&1
go build -o /tmp/b_int_go selfhost/bench/mb_int/main.go
bench_one "int loop (200M)" /tmp/b_int_mfl /tmp/b_int_go 200000000

# 2) substr-heavy loop
"$MACHIN" encode selfhost/bench/mb_str.src > /tmp/b_str.mfl 2>/dev/null \
  && "$MACHIN" build /tmp/b_str.mfl -o /tmp/b_str_mfl >/dev/null 2>&1
go build -o /tmp/b_str_go selfhost/bench/mb_str/main.go
bench_one "substr loop (98M)" /tmp/b_str_mfl /tmp/b_str_go 2000000

# 3) the lexer, real corpus
cat framework/*.src > /tmp/bigcorpus.src 2>/dev/null
cat selfhost/lex.src selfhost/lexbench.src > /tmp/lexbench.src
"$MACHIN" encode /tmp/lexbench.src > /tmp/lexbench.mfl 2>/dev/null \
  && "$MACHIN" build /tmp/lexbench.mfl -o /tmp/mfl-lexbench >/dev/null 2>&1
printf '%-28s MFL: ' "lexer corpus (200×)"; /tmp/mfl-lexbench /tmp/bigcorpus.src 200 2>/dev/null
printf '%-28s Go:  ' ''; "$MACHIN" lexbench /tmp/bigcorpus.src 200 2>/dev/null
