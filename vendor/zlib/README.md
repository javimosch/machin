# Vendored zlib

`zlib-src.tar.gz` — the deflate/inflate subset of **zlib 1.3.1**, from
<https://github.com/madler/zlib/archive/refs/tags/v1.3.1.tar.gz>.

zlib is under the permissive **zlib licence** (see the header of any file inside).

## Why a subset

Only what `zlib_compress` / `zlib_decompress` reach: `compressBound`,
`deflateInit2`/`deflate`/`deflateEnd`, `inflateInit2`/`inflate`/`inflateEnd`, and
their dependencies.

    adler32.c crc32.c deflate.c inflate.c inffast.c inftrees.c trees.c
    zutil.c compress.c   (+ their headers)

Deliberately excluded: the `gz*` file API (`gzread.c`, `gzwrite.c`, `gzlib.c`,
`gzclose.c`), which machin does not expose and which drags in stdio plumbing, and
`infback.c`, used only by the callback inflate API. `gzguts.h` IS included: `zutil.c`
 includes it, though no `gz*.c` is compiled.

## Why it is vendored at all

The **windows target** has no system libz, and requiring one would push a
dependency onto every user of `zlib_compress` (see issue #517). SQLite is bundled
for the same reason and by the same mechanism — zlib is portable C with its own
Windows support, so bundling is the whole port.

The **native** build still links the system `-lz`; nothing about it changes.
