/* Hand-written C reference for the fib(40) benchmark.
 * This is the baseline MFL is measured against: the same naive,
 * doubly-recursive algorithm the compiler emits from fib.mfl.
 *
 *   cc -O2 -o fib_c examples/bench/fib.c
 *   ./fib_c
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
