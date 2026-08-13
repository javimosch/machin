/* Differential oracle: glibc POSIX ERE  vs  vendored Remimu, over the exact
   operations machin's regex_* builtins expose. The question PR #612 turns on is
   empirical -- POSIX ERE is leftmost-LONGEST, a backtracking engine is
   leftmost-FIRST -- so measure how far apart they actually are rather than
   argue about it. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <regex.h>

#define REMIMU_LOG_ERROR(X) ((void)0)
#define REMIMU_ASSERT(X) ((void)0)
#include "remimu.h"   /* the candidate engine; not vendored in this repo */

/* ---- POSIX side: what machin does today on native/wasm ---- */
static int posix_match(const char* s, const char* pat) {
    regex_t re;
    if (regcomp(&re, pat, REG_EXTENDED | REG_NOSUB) != 0) return -1; /* -1 = bad pattern */
    int r = regexec(&re, s, 0, NULL, 0);
    regfree(&re);
    return r == 0;
}
static int posix_find(const char* s, const char* pat, char* out, size_t outcap) {
    regex_t re;
    out[0] = 0;
    if (regcomp(&re, pat, REG_EXTENDED) != 0) return -1;
    regmatch_t m[1];
    int found = 0;
    if (regexec(&re, s, 1, m, 0) == 0 && m[0].rm_so >= 0) {
        size_t len = (size_t)(m[0].rm_eo - m[0].rm_so);
        if (len >= outcap) len = outcap - 1;
        memcpy(out, s + m[0].rm_so, len);
        out[len] = 0;
        found = 1;
    }
    regfree(&re);
    return found;
}

/* ---- Remimu side: exactly the loops PR #612 emits for the windows target ---- */
static int remimu_match(const char* s, const char* pat) {
    RegexToken tokens[1024];
    int16_t token_count = 1024;
    if (regex_parse(pat, tokens, &token_count, 0) != 0) return -1;
    size_t n = strlen(s);
    for (size_t i = 0; i <= n; i++) {
        int64_t end = regex_match(tokens, s, i, 0, NULL, NULL);
        if (end >= 0 && (size_t)end >= i) return 1;
    }
    return 0;
}
static int remimu_find(const char* s, const char* pat, char* out, size_t outcap) {
    RegexToken tokens[1024];
    int16_t token_count = 1024;
    out[0] = 0;
    if (regex_parse(pat, tokens, &token_count, 0) != 0) return -1;
    size_t n = strlen(s);
    for (size_t i = 0; i <= n; i++) {
        int64_t end = regex_match(tokens, s, i, 0, NULL, NULL);
        if (end >= 0 && (size_t)end >= i) {
            size_t len = (size_t)end - i;
            if (len >= outcap) len = outcap - 1;
            memcpy(out, s + i, len);
            out[len] = 0;
            return 1;
        }
    }
    return 0;
}

typedef struct { const char* pat; const char* subj; } Case;

int main(void) {
    static const Case cases[] = {
        /* --- patterns machin's own tests and examples actually use --- */
        {"a", "a"}, {"[0-9]+", "abc"}, {"[0-9]+", "id 4821 x"},
        {"(abc)(x)?", "abc"}, {"^[a-z]+@[a-z]+\\.[a-z]+$", "a@b.co"},
        {"([0-9]+)-([0-9]+)-([0-9]+)", "2026-06-22"},
        {"([a-z]+):([0-9]+):([a-z]+)", "carol:99:admin"},
        {"[0-9]+", "5-9"}, {"#", "a#b"}, {"x", "abc"},

        /* --- realistic field-extraction shapes --- */
        {"[a-z]+", "hello world"}, {"[A-Za-z0-9_]+", "tok_42 rest"},
        {"[0-9]{2,4}", "year 2026 x"}, {"^GET|^POST", "POST /x"},
        {"https?://[a-z.]+", "see http://a.b end"},
        {"[ \t]+", "a   b"}, {"\\.[a-z]+$", "file.tar.gz"},
        {"(foo|foobar)", "foobar"},            /* the classic */
        {"(a|ab)(c|bcd)", "abcd"},             /* POSIX picks the longest OVERALL */
        {"a*", "aaa"}, {"a*", ""}, {"(a*)*", "aaa"},
        {"[[:digit:]]+", "x42y"}, {"[[:alpha:]]+", "42abc"},
        {"^$", ""}, {"^", "abc"}, {"$", "abc"},
        {"a{2}", "aaa"}, {"a{2,}", "aaaa"}, {"(ab)+", "ababab"},
        {"[^a]+", "aaabbb"}, {"a.c", "abc"}, {".*", "abc"},
        {"x|", "abc"}, {"(|a)", "a"},

        /* --- alternation orderings: where leftmost-longest and
               leftmost-first are KNOWN to disagree --- */
        {"b|bc", "abcd"}, {"bc|b", "abcd"},
        {"a|ab", "ab"}, {"ab|a", "ab"},
        {"one|onetwo", "onetwo"}, {"onetwo|one", "onetwo"},
        {"[0-9]|[0-9][0-9]", "42"}, {"[0-9][0-9]|[0-9]", "42"},
        {"x(a|ab)y?", "xaby"},
        {"(a+|a+b)", "aab"},

        /* --- the REVERSE direction: PCRE syntax remimu accepts but POSIX ERE
               treats as a literal, so these would work only on Windows --- */
        {"\\d+", "x42y"}, {"\\w+", "a_b!"}, {"\\s", "a b"},
        {"[[:space:]]", "a b"}, {"[[:upper:]]+", "aBC"},
    };
    const int n = (int)(sizeof(cases) / sizeof(cases[0]));
    int diffs = 0, agree = 0, badpat = 0;

    printf("%-24s %-16s %-12s %-12s %s\n", "PATTERN", "SUBJECT", "POSIX-find", "REMIMU-find", "");
    printf("---------------------------------------------------------------------------------\n");
    for (int i = 0; i < n; i++) {
        char pf[512], rf[512];
        int pm = posix_match(cases[i].subj, cases[i].pat);
        int rm = remimu_match(cases[i].subj, cases[i].pat);
        int pfo = posix_find(cases[i].subj, cases[i].pat, pf, sizeof pf);
        int rfo = remimu_find(cases[i].subj, cases[i].pat, rf, sizeof rf);

        if (pm < 0 || rm < 0 || pfo < 0 || rfo < 0) {
            /* pattern rejected by one or both -- report, don't count as agreement */
            if ((pm < 0) != (rm < 0)) {
                printf("PARSE  %-22s %-16s posix=%s remimu=%s\n", cases[i].pat, cases[i].subj,
                       pm < 0 ? "reject" : "accept", rm < 0 ? "reject" : "accept");
                diffs++;
            } else {
                badpat++;
            }
            continue;
        }
        int same = (pm == rm) && (strcmp(pf, rf) == 0);
        if (same) { agree++; continue; }
        diffs++;
        printf("DIFF   %-22s %-16s [%s]%*s [%s]  match:%d/%d\n",
               cases[i].pat, cases[i].subj, pf, (int)(10 - strlen(pf)), "", rf, pm, rm);
    }
    printf("---------------------------------------------------------------------------------\n");
    printf("agree=%d  diverge=%d  both-reject=%d  total=%d\n", agree, diffs, badpat, n);
    return diffs > 0 ? 1 : 0;
}
