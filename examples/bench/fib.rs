// Rust reference for the fib(40) benchmark — naive doubly-recursive,
// matching the MFL and C implementations for an apples-to-apples compare.
//
//   rustc -O -o fib_rs examples/bench/fib.rs
//   ./fib_rs

fn fib(n: i64) -> i64 {
    if n < 2 { return n; }
    fib(n - 1) + fib(n - 2)
}

fn main() {
    println!("{}", fib(40));
}
