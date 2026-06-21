#!/usr/bin/env bash
# Compose the machweb framework with an app, compile, and run it.
#   ./framework/run.sh framework/example.src
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o machin .
app="${1:-framework/example.src}"
./machin encode framework/machweb.src "$app" > framework/app.mfl
exec ./machin run framework/app.mfl
