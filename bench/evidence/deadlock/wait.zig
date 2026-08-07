// Same logical program: block on a value that can never arrive.
//
// Zig 0.16's own concurrency primitives moved under `std.Io` and now require an
// Io instance, so this uses the POSIX semaphore directly through std.c — stable,
// and unambiguously "wait for something nobody will ever send".
const std = @import("std");
const c = std.c;

pub fn main() void {
    var sem: c.sem_t = undefined;
    _ = c.sem_init(&sem, 0, 0);
    _ = c.sem_wait(&sem); // nothing ever posts it
    std.debug.print("{d}\n", .{@as(i64, 0)});
}
