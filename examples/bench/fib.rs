// Reference Rust baseline for scripts/bench.sh — equivalent to
// examples/bench/fib.mfl (naive double recursion). Compiled with rustc -O.
fn fib(n: u64) -> u64 {
    if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
    let n: u64 = std::env::args().nth(1).and_then(|s| s.parse().ok()).unwrap_or(40);
    println!("{}", fib(n));
}
