#!/usr/bin/env python3
"""Cold-start latency + idle memory for the same hello HTTP server, four ways.

This is the axis where a tiny static native binary doesn't tie — it wins by a wide
margin. For each server we measure:

  COLD START  exec -> first successful HTTP 200, busy-polled (min of N, least noise).
  IDLE RSS    resident memory once it's serving (/proc/<pid>/status VmRSS).

Run ./build.sh first. Node/Python are skipped if their runtime isn't installed.
Docker image sizes (the real 'what you ship' number) come from ./images.sh.
"""
import os
import socket
import subprocess
import time

HERE = os.path.dirname(os.path.abspath(__file__))
RUNS = 15

# name, argv, port
SERVERS = [
    ("machin", [os.path.join(HERE, "hello-machin")], 8090),
    ("go", [os.path.join(HERE, "hello-go")], 8091),
    ("node", ["node", os.path.join(HERE, "hello.js")], 8092),
    ("python", ["python3", os.path.join(HERE, "hello.py")], 8093),
]


def try_get(port):
    try:
        s = socket.create_connection(("127.0.0.1", port), timeout=0.05)
        s.sendall(b"GET / HTTP/1.0\r\n\r\n")
        ok = b"200" in s.recv(64)
        s.close()
        return ok
    except OSError:
        return False


def rss_kb(pid):
    try:
        with open(f"/proc/{pid}/status") as f:
            for line in f:
                if line.startswith("VmRSS:"):
                    return int(line.split()[1])
    except OSError:
        pass
    return None


def measure(argv, port):
    best = None
    rss = None
    for _ in range(RUNS):
        t0 = time.perf_counter()
        p = subprocess.Popen(argv, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        while True:
            if try_get(port):
                dt = time.perf_counter() - t0
                break
            if time.perf_counter() - t0 > 10:
                dt = None
                break
        if dt is not None:
            best = dt if best is None else min(best, dt)
            if rss is None:
                rss = rss_kb(p.pid)
        p.terminate()
        try:
            p.wait(timeout=2)
        except subprocess.TimeoutExpired:
            p.kill()
        time.sleep(0.03)
    return best, rss


def have(argv):
    if argv[0].startswith("/"):
        return os.path.exists(argv[0])
    return subprocess.run(["which", argv[0]], capture_output=True).returncode == 0


def main():
    print(f"cold start = exec -> first HTTP 200 (min of {RUNS}); RSS = idle resident memory\n")
    print(f"{'server':<8} {'cold start':>12} {'idle RSS':>10}")
    print("-" * 34)
    base = None
    rows = []
    for name, argv, port in SERVERS:
        if not have(argv):
            print(f"{name:<8} {'(not built/installed)':>23}")
            continue
        dt, rss = measure(argv, port)
        rows.append((name, dt, rss))
        if name == "machin":
            base = dt
        ms = f"{dt*1000:.2f} ms" if dt else "n/a"
        mem = f"{rss/1024:.1f} MB" if rss else "n/a"
        print(f"{name:<8} {ms:>12} {mem:>10}")
    if base:
        print("\nrelative to machin:")
        for name, dt, _ in rows:
            if dt:
                print(f"  {name:<8} {dt/base:6.1f}x")


if __name__ == "__main__":
    main()
