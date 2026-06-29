fn main() {
    let n = 10_000_000usize;
    let mut sieve: Vec<i64> = Vec::new();
    let mut i = 0usize;
    while i <= n { sieve.push(1); i += 1; }
    sieve[0] = 0;
    sieve[1] = 0;
    let mut p = 2usize;
    while p * p <= n {
        if sieve[p] == 1 {
            let mut m = p * p;
            while m <= n { sieve[m] = 0; m += p; }
        }
        p += 1;
    }
    let mut count: i64 = 0;
    let mut k = 0usize;
    while k <= n { count += sieve[k]; k += 1; }
    println!("{}", count);
}
