# "watch machin compile itself" — 60-second proof clip

A shareable terminal recording of the self-hosting fixpoint: machin compiles its own
compiler to a native binary, that binary re-compiles the same source, and the two C
outputs are byte-for-byte identical — then a note that it runs as fast as the original.

## Reproduce
```bash
# from repo root: build the toolchain, then record
GOMAXPROCS=4 ./bin/machin encode selfhost/lex.src selfhost/parse.src selfhost/check.src \
  selfhost/checkgen.src selfhost/cgen.src selfhost/cgbuiltin.src selfhost/cgagg.src \
  selfhost/cgffi.src selfhost/cgprelude.src selfhost/cgprog.src selfhost/cgmain.src > compiler.mfl
./bin/machin build compiler.mfl -o mfl-cgen
# copy the 10 hand-written sources into ./src/, place compiler.mfl + mfl-cgen alongside record-demo.sh, then:
asciinema rec --overwrite -c ./record-demo.sh demo.cast
agg --font-size 26 --theme monokai --line-height 1.35 demo.cast demo.gif
ffmpeg -y -i demo.gif -movflags faststart -pix_fmt yuv420p -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" demo.mp4
```

Deliverables live at `docs/self-hosting-demo.{gif,mp4}`.
