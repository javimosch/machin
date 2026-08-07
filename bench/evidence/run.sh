#!/usr/bin/env bash
# Benchmark — what the COMPILER tells you, machin vs Rust vs Zig.
#
# native-speed and compile-speed measure time. This measures something you cannot
# put a millisecond on: which toolchain hands you the bug, and when.
#
#   deadlock/  block on a value that can never arrive
#   oob/       index a slice with an index taken from the environment
#
# For each we record the verdict at BUILD time and at RUN time. Programs that hang
# are killed with `timeout` — a kill is itself the result, not a harness error.
#
# NOTE ON EXIT CODES: every status here is captured with `cmd; rc=$?` on the
# command itself, never `cmd | sed; echo $?` — the latter reports the status of
# the last pipeline stage (sed), which silently turns a failed build into "exit 0".
# An earlier version of this script had exactly that bug and reported broken Zig
# builds as successes.
set +e
cd "$(dirname "$0")"

for t in machin rustc zig; do
  command -v "$t" >/dev/null || { echo "missing: $t"; exit 1; }
done

B=build
rm -rf "$B" && mkdir -p "$B"
hr() { printf '%.0s-' {1..74}; echo; }
say() { printf '\n== %s\n' "$1"; hr; }
indent() { sed 's/^/    /'; }

# ------------------------------------------------------------- deadlock --
say "CASE 1 - deadlock: block on a value that can never arrive"

machin encode deadlock/wait.src > "$B/wait.mfl" 2>/dev/null

echo "[machin] compile-time analysis (machin check):"
out=$(machin check "$B/wait.mfl" 2>&1); rc=$?
echo "$out" | indent
echo "    -> check exit=$rc"

machin build "$B/wait.mfl" -o "$B/m-wait" >/dev/null 2>&1
echo "[machin] run (5s budget):"
out=$(timeout 5 "$B/m-wait" 2>&1); rc=$?
echo "$out" | indent
echo "    -> exit=$rc   (2 = deadlock detected and reported; 124 = still hanging)"

echo
echo "[rust] build:"
out=$(rustc -C opt-level=3 deadlock/wait.rs -o "$B/r-wait" 2>&1); rc=$?
[ -n "$out" ] && echo "$out" | indent
echo "    -> build exit=$rc   (0 = accepted, no diagnostic about the deadlock)"
echo "[rust] run (5s budget):"
timeout 5 "$B/r-wait" >/dev/null 2>&1; rc=$?
echo "    -> exit=$rc   (124 = killed by timeout, i.e. hung forever)"

echo
echo "[zig] build:"
out=$(zig build-exe -OReleaseFast -lc deadlock/wait.zig -femit-bin="$B/z-wait" 2>&1); rc=$?
[ -n "$out" ] && echo "$out" | indent
echo "    -> build exit=$rc   (0 = accepted, no diagnostic about the deadlock)"
echo "[zig] run (5s budget):"
timeout 5 "$B/z-wait" >/dev/null 2>&1; rc=$?
echo "    -> exit=$rc   (124 = killed by timeout, i.e. hung forever)"

# ------------------------------------------------------------------ oob --
say "CASE 2 - out-of-range index, found before it is ever hit"

machin encode oob/at.src > "$B/at.mfl" 2>/dev/null

echo "[machin] compile-time bug-finder (machin falsify):"
out=$(machin falsify "$B/at.mfl" 2>&1); rc=$?
echo "$out" | indent
echo "    -> falsify exit=$rc"

echo
echo "[rust] build - any diagnostic about at()?"
out=$(rustc -C opt-level=3 oob/at.rs -o "$B/r-at" 2>&1); rc=$?
[ -n "$out" ] && echo "$out" | indent || echo "    (no output)"
echo "    -> build exit=$rc   (silent: bounds are a runtime concern in Rust)"

echo
echo "[zig] build - any diagnostic about at()?"
out=$(zig build-exe -OReleaseFast -lc oob/at.zig -femit-bin="$B/z-at" 2>&1); rc=$?
[ -n "$out" ] && echo "$out" | indent || echo "    (no output)"
echo "    -> build exit=$rc   (silent)"

echo
echo "Run each with the in-range index the author tested (1), then with 5."
echo "The array has 3 elements, so 5 is out of range:"
echo
# Show machin BOTH ways. Its default build omits bounds checks exactly like Zig's
# ReleaseFast, so comparing machin --safe against Zig ReleaseFast would flatter
# machin. Rust keeps its bounds check in release, and that is a real Rust win at
# runtime — machin's advantage here is the COMPILE-time report above, not this table.
machin build "$B/at.mfl" -o "$B/m-at-default" >/dev/null 2>&1
machin build "$B/at.mfl" -o "$B/m-at" --safe >/dev/null 2>&1
zig build-exe -OReleaseSafe -lc oob/at.zig -femit-bin="$B/z-at-safe" >/dev/null 2>&1
printf '  %-16s %-10s %-36s %s\n' "" "IDX=1" "IDX=5 (out of range)" "exit"
for pair in "machin (default):$B/m-at-default" "machin --safe:$B/m-at" "rust (release):$B/r-at" "zig ReleaseFast:$B/z-at" "zig ReleaseSafe:$B/z-at-safe"; do
  name="${pair%%:*}"; bin="${pair#*:}"
  if [ ! -x "$bin" ]; then printf '  %-16s %s\n' "$name" "(build failed)"; continue; fi
  # Capture status from the program itself, not from a pipeline stage; and skip
  # blank lines, because Rust's panic output starts with one.
  g_out=$(IDX=1 timeout 5 "$bin" 2>&1)
  b_out=$(IDX=5 timeout 5 "$bin" 2>&1); rc=$?
  good=$(printf '%s\n' "$g_out" | grep -v '^[[:space:]]*$' | head -1)
  bad=$(printf '%s\n' "$b_out" | grep -v '^[[:space:]]*$' | head -1)
  printf '  %-16s %-10s %-36s %s\n' "$name" "$good" "${bad:-<no output>}" "$rc"
done

say "DONE"
echo "See README.md for the reading of these results."
