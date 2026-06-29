fn main() {
    let n: i64 = 1_000_000_000;
    let md: i64 = 1_000_000_007;
    let mut s: i64 = 0;
    let mut i: i64 = 1;
    while i <= n {
        s = (s + i * i) % md;
        i += 1;
    }
    println!("{}", s);
}
