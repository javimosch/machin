/* Reference hand-written baseline for scripts/bench.sh — must stay equivalent
 * to examples/bench/fib.mfl (naive double recursion). Compiled with cc -O2. */
#include <stdio.h>
#include <stdlib.h>

long fib(long n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}

int main(int argc, char **argv) {
    long n = (argc > 1) ? atol(argv[1]) : 40;
    printf("%ld\n", fib(n));
    return 0;
}
