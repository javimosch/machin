/* Hand-written reference for the fib(40) benchmark.
 * Naive doubly-recursive fibonacci — the same algorithm the MFL program in
 * fib.mfl compiles to, so the two are a fair compiler-vs-compiler comparison.
 * Build: cc -O2 -o fib_c fib.c
 */
#include <stdio.h>

static long fib(long n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}

int main(void) {
    printf("%ld\n", fib(40));
    return 0;
}
