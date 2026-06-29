fn main() {
    let w = 1000i64;
    let h = 1000i64;
    let maxit = 1000i64;
    let mut total: i64 = 0;
    let mut py = 0i64;
    while py < h {
        let mut px = 0i64;
        while px < w {
            let x0 = (px as f64 / w as f64) * 3.5 - 2.5;
            let y0 = (py as f64 / h as f64) * 2.0 - 1.0;
            let mut x = 0.0f64;
            let mut y = 0.0f64;
            let mut it = 0i64;
            while it < maxit {
                let x2 = x * x;
                let y2 = y * y;
                if x2 + y2 > 4.0 { break; }
                y = 2.0 * x * y + y0;
                x = x2 - y2 + x0;
                it += 1;
            }
            total += it;
            px += 1;
        }
        py += 1;
    }
    println!("{}", total);
}
