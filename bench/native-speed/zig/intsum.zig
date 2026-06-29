const std = @import("std");

pub fn main() void {
    const n: i64 = 1_000_000_000;
    const md: i64 = 1_000_000_007;
    var s: i64 = 0;
    var i: i64 = 1;
    while (i <= n) : (i += 1) {
        s = @rem(s + i * i, md);
    }
    std.debug.print("{d}\n", .{s});
}
