#!/usr/bin/env bash
# Build the four hello servers as deployable artifacts.
#   machin : fully static via musl (CC=./muslcc) -> runs FROM scratch
#   Go     : fully static (CGO_ENABLED=0)        -> runs FROM scratch
#   Node   : one .js file + the node:alpine image
#   Python : one .py file + the python:alpine image
set +e
cd "$(dirname "$0")"
chmod +x muslcc

echo "== machin (static musl) =="
machin encode ../../framework/machweb.src hello.src > hello.mfl 2>/dev/null
CC="$PWD/muslcc" machin build hello.mfl -o hello-machin 2>/dev/null \
  && echo "  hello-machin: $(stat -c%s hello-machin) bytes ($(file -b hello-machin | grep -o 'statically linked\|dynamically linked'))" \
  || echo "  machin build FAIL (need musl-gcc)"

echo "== Go (static) =="
CGO_ENABLED=0 go build -ldflags='-s -w' -o hello-go hello.go 2>/dev/null \
  && echo "  hello-go: $(stat -c%s hello-go) bytes ($(file -b hello-go | grep -o 'statically linked\|dynamically linked'))" \
  || echo "  go build FAIL"

echo "== Node / Python artifacts =="
echo "  hello.js: $(stat -c%s hello.js) bytes (+ node runtime)"
echo "  hello.py: $(stat -c%s hello.py) bytes (+ CPython runtime)"
