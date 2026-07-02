// The naive translation, no `unsafe` anywhere. Rust's compiler flatly REFUSES
// to build this — E0133, "use of mutable static is unsafe" — naming the exact
// danger before the program ever runs.
use std::thread;

static mut COUNTER: i64 = 0;

fn incr() {
    for _ in 0..2_000_000 {
        COUNTER = COUNTER + 1;
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
    println!("{}", COUNTER);
}
