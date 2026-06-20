/* Hand-written C baseline for the fib(40) benchmark.
 * This is the same algorithm the MFL compiler emits for examples/bench/fib.mfl,
 * so it serves as the "is MFL as fast as C?" reference point.
 * Build: cc -O2 fib.c -o fib_c   (see run_bench.sh) */
#include <stdio.h>

static long fib(long n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}

int main(void) {
    printf("%ld\n", fib(40));
    return 0;
}
