#!/usr/bin/env bash
# Measure a --static TLS-using binary: does it really run FROM scratch, and how
# big is it? Extends bench/cold-start (which only covers the libc+SQLite case) to
# the more common shape: an app that actually calls out over HTTPS. See issue #283.
set -e
cd "$(dirname "$0")"

machin encode hello.src > hello.mfl

echo "== build (dynamic) =="
machin build hello.mfl -o hello-dynamic
echo "  hello-dynamic: $(stat -c%s hello-dynamic) bytes ($(file -b hello-dynamic | grep -o 'statically linked\|dynamically linked'))"

echo "== build (--static: glibc, needs libssl-dev's static archives) =="
machin build --static hello.mfl -o hello-static
strip -o hello-static-stripped hello-static
echo "  hello-static (unstripped): $(stat -c%s hello-static) bytes"
echo "  hello-static (stripped):   $(stat -c%s hello-static-stripped) bytes ($(file -b hello-static-stripped | grep -o 'statically linked\|dynamically linked'))"

echo "== run it (real network + TLS handshake + cert verification) =="
./hello-static-stripped

if command -v docker >/dev/null 2>&1; then
  echo "== FROM-scratch acid test (Docker, zero other files in the image) =="
  cat > Dockerfile.scratch <<'EOF'
FROM scratch
COPY hello-static-stripped /app
ENTRYPOINT ["/app"]
EOF
  docker build -q -f Dockerfile.scratch -t machin-bench-tls-static . >/dev/null
  docker run --rm machin-bench-tls-static
  docker rmi -f machin-bench-tls-static >/dev/null
  rm -f Dockerfile.scratch
else
  echo "  (docker not found — skipping the FROM-scratch container test)"
fi

rm -f hello-dynamic hello-static hello-static-stripped hello.mfl 2>/dev/null || true
