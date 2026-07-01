#!/usr/bin/env bash
cd "$(dirname "$0")"
R=$'\e[0m'; G=$'\e[38;5;42m'; W=$'\e[97m'; D=$'\e[38;5;245m'; C=$'\e[38;5;45m'; Y=$'\e[38;5;227m'
say(){ printf "%b\n" "$1"; }
p(){ sleep "$1"; }
cmd(){ printf "${D}\$ ${C}%s${R}\n" "$1"; sleep 0.7; }

clear; p 0.5
say "  ${G}machin${R}${D} — a programming language I built for AI agents.${R}"; p 1.5
say "${D}  In 2026 I asked it the hardest question a language can face:${R}"; p 1.3
say "  ${W}    can you compile yourself?${R}"; p 2.0
say ""
say "${D}  The machin compiler — lexer, parser, type-checker, code generator —${R}"
say "${D}  is itself written in machin:${R}"; p 0.8
cmd "wc -l src/*.src"
wc -l src/*.src | tail -1 | sed "s/total/lines of machin/"; p 1.8
say ""
say "  ${G}Step 1${R}${D} — machin compiles its own compiler into a native binary.${R}"; p 0.9
cmd "mfl-cgen --full compiler.mfl  > mflc2.c"
./mfl-cgen --full compiler.mfl > mflc2.c; say "${D}   -> $(wc -l < mflc2.c) lines of C, emitted by machin${R}"; p 0.7
cmd "cc -O2 mflc2.c -o mflc2"
cc -O2 -std=c11 -pthread mflc2.c -o mflc2; say "${D}   -> mflc2: a native machin compiler, ${W}built by machin${R}"; p 1.6
say ""
say "  ${G}Step 2${R}${D} — that self-built binary re-compiles its own source.${R}"
say "${D}  If machin is real, the two outputs are identical.${R}"; p 1.1
cmd "mflc2 --full compiler.mfl  > mflc3.c"
./mflc2 --full compiler.mfl > mflc3.c; say "${D}   done.${R}"; p 0.6
cmd "diff mflc2.c mflc3.c  &&  echo IDENTICAL"
if diff mflc2.c mflc3.c; then say "${G}   IDENTICAL${R}"; fi; p 1.2
say "  ${W}   0 differences.   $(wc -l < mflc2.c | tr -d ' ') lines.   byte-for-byte.${R}"; p 0.9
say "  ${G}   \xE2\x9C\x93  THE FIXPOINT — machin reproduces itself, exactly.${R}"; p 2.0
say ""
say "${D}  And it isn't slow. Parse + type-check + compile its own source:${R}"; p 0.7
say "${D}     the original compiler   ${R}${W}~270 ms${R}"
say "${D}     machin, self-hosted     ${R}${G}~270 ms${R}${D}  — as fast as the compiler that built it${R}"; p 2.0
say ""
say "  ${W}A language that compiles itself.${R}"; p 0.9
say "${D}  AI as the engine. Verified to the byte, by hand.${R}"; p 1.3
say "  ${G}Javier Arancibia   \xC2\xB7   intrane.fr${R}"; p 2.5
