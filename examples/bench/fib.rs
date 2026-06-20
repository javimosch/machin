// Rust reference for the fib(40) benchmark — same naive recursion as fib.mfl / fib.c.
// Build: rustc -O fib.rs -o fib_rs   (see run_bench.sh)

fn fib(n: i64) -> i64 {
    if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
    println!("{}", fib(40));
}
