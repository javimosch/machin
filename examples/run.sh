#!/usr/bin/env bash
# Run every MFL program. The .mfl files are the source of truth — canonical
# plain text, one normalized function per line.
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o machin .

find examples -name '*.mfl' | sort | while read -r mfl; do
    case "$mfl" in
        *server*|*_api*) echo "########## $mfl (long-running — skipped) ##########"; echo; continue ;;
    esac
    echo "########## machin run $mfl ##########"
    ./machin run "$mfl"
    echo
done
