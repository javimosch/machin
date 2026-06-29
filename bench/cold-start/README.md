# Benchmark — cold start, memory & ship size

The two other benchmarks show machin *ties or beats* the native tier on speed and
matches Python on agent-write cost. This is the axis where it **doesn't tie** — a
tiny static native binary wins ship-size, cold-start, and memory by 1–3 orders of
magnitude over a containerized interpreter.

Same minimal HTTP "hello" server in each language (isolates the platform's floor,
not app code), measured four ways.

## Results (this machine — Docker 28, node 24, python 3.11)

| | deployable image | cold start† | idle RSS | what you ship |
|---|--:|--:|--:|---|
| **machin** | **92.9 kB** | **0.49 ms** | **108 kB** | one static binary, `FROM scratch` |
| Go | 4.77 MB | 1.70 ms | 5.3 MB | one static binary, `FROM scratch` |
| Node | 178 MB | 28.9 ms | 51 MB | `.js` + `node:alpine` |
| Python | 47.6 MB | 49.1 ms | 17.9 MB | `.py` + `python:alpine` |

† cold start = `exec` → first HTTP 200, busy-polled, min of 15.

**Relative to machin:**

| | image | cold start | idle RSS |
|---|--:|--:|--:|
| Go | 51× | 3.5× | 49× |
| Python | 512× | 100× | 166× |
| Node | **1916×** | 59× | **477×** |

The machin image is the **binary and nothing else** — verified to serve traffic
from a `FROM scratch` container (no libc, no shell, no `/etc`). That's a 92.9 kB
image with a ~0.1 MB resident footprint that's ready in half a millisecond.

## Why this matters for the north star

machin's audience is the SME backend you drop on a small VPS or a scale-to-zero
platform. There, this axis is the product:

- **Ship**: `scp` a 92.9 kB binary, or push a 92.9 kB image. No `node_modules`, no
  `pip install`, no 178 MB pull on every cold node.
- **Density**: 108 kB resident means hundreds of services per box, not dozens.
- **Scale-to-zero / FaaS**: a sub-millisecond cold start has no warm-pool tax.

It's *as terse as Python to write* (see [`../rest-sqlite`](../rest-sqlite)) and
*native-tier fast* (see [`../native-speed`](../native-speed)) — and it ships like
this.

## How machin ships static

By default `machin build` produces a small **dynamically-linked** glibc binary
(~27 kB for this server). For a `FROM scratch` image, link it statically with musl
— machin honors `$CC`, so a one-line wrapper does it:

```sh
# muslcc:  exec musl-gcc -static "$@"
CC=./muslcc machin build hello.mfl -o hello-machin     # -> 92.9 kB, statically linked
```

machin is just C underneath, so this is the standard static-musl trick, nothing
machin-specific.

## Reproduce

```bash
./build.sh      # build the static machin + Go binaries; report artifact sizes
python3 measure.py   # cold-start + idle RSS (node/python skipped if not installed)
./images.sh     # build deployable Docker images, print sizes  (needs Docker)
```
