#!/usr/bin/env bash
# Build a deployable Docker image for each hello server and print the sizes —
# the real "what you ship" number. machin & Go go FROM scratch; Node & Python
# carry their interpreter's base image. Run ./build.sh first (needs the binaries).
set +e
cd "$(dirname "$0")"

for v in machin go node python; do
  docker build -q -f $v.Dockerfile -t coldstart-$v . >/dev/null 2>build-$v.err \
    && echo "built coldstart-$v" || { echo "coldstart-$v FAILED (see build-$v.err)"; }
done

echo
echo "deployable image sizes:"
docker images --format '{{.Repository}} {{.Size}}' | grep '^coldstart-' | sort
