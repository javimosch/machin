// Same function: index a slice with a caller-supplied index.
// Rust bounds-checks at RUNTIME (panic). There is no compile-time report, and no
// tool in the Rust toolchain hands you the failing input before you run it.
fn at(xs: &[i64], i: usize) -> i64 {
    xs[i]
}

fn main() {
    let xs = [10i64, 20, 30];
    let i: usize = std::env::var("IDX").ok().and_then(|s| s.parse().ok()).unwrap_or(1);
    println!("{}", at(&xs, i));
}
