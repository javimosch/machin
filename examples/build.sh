#!/usr/bin/env bash
# Compile every readable .mfs source into the real MFL (.mfl base64) form,
# then run each one. The .mfl files are the actual programs; .mfs is just the
# human-readable projection used for authoring.
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o machin .

find examples -name '*.mfs' | sort | while read -r src; do
    mfl="${src%.mfs}.mfl"
    ./machin encode "$src" > "$mfl"
    echo "########## machin run $mfl ##########"
    ./machin run "$mfl"
    echo
done
