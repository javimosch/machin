/* Hand-written C baseline that MFL's fib.mfl compiles to.
 * Build: cc -O2 fib.c -o fib_c
 * This is the "hand-written C (cc -O2)" row in the README Performance table. */
#include <stdio.h>

static long fib(long n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}

int main(void) {
    printf("%ld\n", fib(40));
    return 0;
}
