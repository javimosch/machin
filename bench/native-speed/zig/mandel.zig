const std = @import("std");

pub fn main() void {
    const w: i64 = 1000;
    const h: i64 = 1000;
    const maxit: i64 = 1000;
    var total: i64 = 0;
    var py: i64 = 0;
    while (py < h) : (py += 1) {
        var px: i64 = 0;
        while (px < w) : (px += 1) {
            const x0 = (@as(f64, @floatFromInt(px)) / @as(f64, @floatFromInt(w))) * 3.5 - 2.5;
            const y0 = (@as(f64, @floatFromInt(py)) / @as(f64, @floatFromInt(h))) * 2.0 - 1.0;
            var x: f64 = 0.0;
            var y: f64 = 0.0;
            var it: i64 = 0;
            while (it < maxit) : (it += 1) {
                const x2 = x * x;
                const y2 = y * y;
                if (x2 + y2 > 4.0) break;
                y = 2.0 * x * y + y0;
                x = x2 - y2 + x0;
            }
            total += it;
        }
    }
    std.debug.print("{d}\n", .{total});
}
