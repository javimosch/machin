// Hand-written reference for the fib(40) benchmark.
// Naive doubly-recursive fibonacci — same algorithm as fib.mfl / fib.c.
// Build: rustc -O -o fib_rs fib.rs
fn fib(n: i64) -> i64 {
    if n < 2 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}

fn main() {
    println!("{}", fib(40));
}
