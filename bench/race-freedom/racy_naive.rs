// racy_no_unsafe.rs made to compile by wrapping every access in `unsafe` — an
// explicit, honest admission "I've turned off the compiler's guarantee here."
// Rust still warns about it. Does NOT reliably demonstrate visible corruption
// (see the README) — that's the point: numeric output isn't proof of safety.
use std::thread;

static mut COUNTER: i64 = 0;

fn incr() {
    for _ in 0..2_000_000 {
        unsafe {
            COUNTER = COUNTER + 1;
        }
    }
}

fn main() {
    let mut handles = vec![];
    for _ in 0..4 {
        handles.push(thread::spawn(incr));
    }
    for h in handles {
        h.join().unwrap();
    }
    unsafe {
        println!("{}", COUNTER);
    }
}
