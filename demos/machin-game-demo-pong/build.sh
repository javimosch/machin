#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
MACHIN=${MACHIN:-../../bin/machin}
$MACHIN encode src/main.src > app.mfl
$MACHIN build app.mfl -o app
echo "built: ./app  →  run in a terminal: ./app"
