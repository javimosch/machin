# regex differential oracle

Any candidate regex engine for the windows target (#517) must answer **exactly**
what `<regex.h>` answers on native, or the same MFL program silently returns
different results per platform. This measures that.

```sh
cp /path/to/candidate-engine.h remimu.h     # or edit the include
cc -O1 -std=c11 -w diff.c -o diff && ./diff # exit 0 = zero divergences
```

## Why it exists

PR #612 proposed vendoring [Remimu](https://github.com/wareya/Remimu), a
backtracking engine, for the windows target while native kept POSIX ERE. The
objection was semantic, so it was measured rather than argued. **13 of 50
divergences**, in three classes:

| class | example | native (POSIX) | Remimu |
|---|---|---|---|
| leftmost-**longest** vs leftmost-**first** | `b\|bc` on `abcd` | `bc` | `b` |
| POSIX bracket classes unsupported | `[[:digit:]]+` on `x42y` | `42` | *no match* |
| PCRE escapes POSIX treats as literals | `\d+` on `x42y` | *no match* | `42` |

The first is the classic ERE/backtracking split: POSIX takes the longest
alternative, a backtracking engine takes the first that succeeds. The second is
worse than a different answer — a program filtering on `[[:digit:]]+` finds
**nothing** on Windows. The third runs the other way: a pattern that works only
on Windows.

## The bar

A replacement engine must be **leftmost-longest** and support POSIX bracket
expressions. A DFA/TNFA implementation (musl's `regcomp`/`regexec`, TRE) meets
that; a PCRE-style backtracker does not, whatever its other merits.

The cheapest sound option is one engine on **every** target, so the two can't
drift — but that changes native behaviour too, so it must clear this harness at
zero divergences first.
