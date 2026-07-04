#!/usr/bin/env bash
# Run every non-server MFL program. The .mfl files are the source of truth —
# canonical plain text, one normalized function per line. Servers/APIs
# (matching *server* or *_api*) are long-running and skipped below.
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o machin .

find examples -name '*.mfl' | sort | while read -r mfl; do
    case "$mfl" in
        *server*|*_api*) echo "########## $mfl (long-running — skipped) ##########"; echo; continue ;;
        examples/gui/*) echo "########## $mfl (GUI — needs raylib + a display; see examples/gui/README.md) ##########"; echo; continue ;;
    esac
    echo "########## machin run $mfl ##########"
    ./machin run "$mfl"
    echo
done
