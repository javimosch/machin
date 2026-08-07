// Same logical program: block on a value that can never arrive.
// The sender is kept alive in scope, so recv() cannot return Err — it blocks forever.
use std::sync::mpsc;

fn main() {
    let (tx, rx) = mpsc::channel::<i64>();
    let _keep_sender_alive = &tx;
    let v = rx.recv().unwrap();
    println!("{}", v);
}
