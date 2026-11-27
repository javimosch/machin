# MFL as a library in an Android app

```sh
export ANDROID_HOME=~/Android/Sdk          # with ndk/<version>; or ANDROID_NDK_HOME
machin build lib.mfl --target android-lib --java-package com.example.app -o gen
```

writes `gen/jniLibs/<abi>/libmfl.so` and `gen/java/com/example/app/{Mfl,MflPanic}.java`.
Add those two directories to the app's native-library and Java sources. `--abi`
takes a comma-separated list of `arm64-v8a` (default), `armeabi-v7a`, `x86_64`.
The library targets API 27 (Android 8.1) and later.

```
export func greet(name) (r) { r = "hello " + name }     // Mfl.greet(String) -> String
```

| MFL | Java |
|---|---|
| int, float, bool | `long`, `double`, `boolean` |
| string | `String` (an MFL string ends at the first NUL; `null` throws) |
| bytes | `byte[]` |
| no result | `void` |

Anything else (slices, maps, structs, several results) is refused: pass JSON in a
string. An export needs exactly one set of types, and its name must be a Java
method name that does not start with `mfl`. The types come from inference, so
constrain a parameter the body does not otherwise pin down (it defaults to int).
Calls block their thread. Globals are shared by all calls and are not
synchronized for you.

## Locale

MFL runs in the `C` locale, whatever the app's locale is, so patterns, number
formatting and parsing, and `to_upper`/`to_lower` behave as in a standalone
program. Only the calling thread is switched, and it is restored when the call
returns. Without this, `[a-z]` could match `é`, `json(2.5)` produce `2,5` and
`parse_float("1.5")` return 1.

## Panics

The library is always built `--safe`. A panic does not exit the app:

- **On the calling thread:** the call throws `MflPanic`.
- **On a goroutine, while the call that started it (directly or indirectly) is
  running:** only that goroutine ends. Goroutines of the same call waiting in a
  receive or `select` give up too, and the call throws `MflPanic`, even if
  nothing was waiting. Other calls are not affected.
- **On a goroutine whose call already returned:** only that goroutine ends. Read
  `Mfl.mflBackgroundPanicCount()` and `Mfl.mflLastBackgroundPanic()`.

## What can still go wrong

- A call waiting on a goroutine started by **another** call hangs if that
  goroutine panics. So does one waiting on a goroutine that died after its own
  call returned.
- There is **no deadlock detector** in a library (it cannot see Java threads that
  may still send), so a real deadlock hangs.
- A goroutine that is busy, or blocked in I/O, is not stopped by a sibling's panic.
- A panic does not undo anything: globals may be half updated, and the memory of
  `arena { }` blocks open at that moment is not freed.
- `exit()` ends the app, as does running out of memory. A panic in a global
  initializer happens while the library loads and also ends the app.
- Channels are not freed automatically (#724); `chan_free` does it by hand, and
  using it wrongly can end or hang the app — a `--safe` build turns that into a
  catchable panic instead.
- Not supported: TLS, OpenSSL crypto, XEdDSA and SQLite (the NDK ships none of
  them). The self-hosted compiler has no android-lib target.
- `exec()` needs a writable `TMPDIR`.
