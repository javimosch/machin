// Same function: index a slice with a caller-supplied index.
// In ReleaseFast, Zig elides the bounds check — an out-of-range index is undefined
// behavior: it silently reads whatever is adjacent in memory.
//
// The index comes from the environment (via libc getenv) because Zig 0.16 has no
// stable global argv: std.os.argv is gone and std.process's iterators want an
// allocator/Io instance.
const std = @import("std");

fn at(xs: []const i64, i: usize) i64 {
    return xs[i];
}

pub fn main() void {
    const xs = [_]i64{ 10, 20, 30 };
    var i: usize = 1;
    if (std.c.getenv("IDX")) |e| {
        i = std.fmt.parseInt(usize, std.mem.span(e), 10) catch 1;
    }
    std.debug.print("{d}\n", .{at(&xs, i)});
}
