#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
MACHIN=${MACHIN:-../../bin/machin}
$MACHIN encode ../../framework/machweb.src src/main.src > app.mfl
$MACHIN build app.mfl -o app
echo "built: ./app  →  run with: ./app"
