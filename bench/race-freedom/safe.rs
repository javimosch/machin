// The actually-safe idiomatic Rust fix: Arc<AtomicI64> + Ordering — real
// additional types/syntax over the naive shape (the "tax" the pitch refers
// to), vs safe.src's zero-annotation channel-based fix in machin.
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use std::thread;

fn incr(counter: Arc<AtomicI64>) {
    for _ in 0..2_000_000 {
        counter.fetch_add(1, Ordering::SeqCst);
    }
}

fn main() {
    let counter = Arc::new(AtomicI64::new(0));
    let mut handles = vec![];
    for _ in 0..4 {
        let c = Arc::clone(&counter);
        handles.push(thread::spawn(move || incr(c)));
    }
    for h in handles {
        h.join().unwrap();
    }
    println!("{}", counter.load(Ordering::SeqCst));
}
