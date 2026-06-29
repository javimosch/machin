const std = @import("std");

pub fn main() !void {
    const n: usize = 10_000_000;
    const alloc = std.heap.page_allocator;
    const sieve = try alloc.alloc(i64, n + 1);
    defer alloc.free(sieve);
    var i: usize = 0;
    while (i <= n) : (i += 1) sieve[i] = 1;
    sieve[0] = 0;
    sieve[1] = 0;
    var p: usize = 2;
    while (p * p <= n) : (p += 1) {
        if (sieve[p] == 1) {
            var m: usize = p * p;
            while (m <= n) : (m += p) sieve[m] = 0;
        }
    }
    var count: i64 = 0;
    var k: usize = 0;
    while (k <= n) : (k += 1) count += sieve[k];
    std.debug.print("{d}\n", .{count});
}
