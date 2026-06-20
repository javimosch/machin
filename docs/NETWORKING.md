# MFL Networking & Concurrency

MFL ships TCP networking built-ins and a `go` keyword for goroutines, so a small
program can serve many connections concurrently while still compiling to a single
native binary. This page documents the socket built-ins and walks through the
concurrent HTTP server in [`examples/complex/http_server.mfl`](../examples/complex/http_server.mfl).

For the rest of the language (types, operators, slices, control flow), see
[`docs/LANGUAGE.md`](LANGUAGE.md).

## Socket built-ins

| Built-in            | Returns | Purpose                                              |
|---------------------|---------|------------------------------------------------------|
| `listen(port)`      | server  | bind and listen on a TCP `port`, return a listener   |
| `accept(server)`    | conn    | block until a client connects, return the connection |
| `read(conn)`        | string  | read the bytes the client sent                       |
| `write(conn, s)`    | —       | write the string `s` back to the client              |
| `close(conn)`       | —       | close the connection                                 |

## Goroutines

The `go` keyword runs a function call concurrently instead of waiting for it:

```go
func work(id, n) { sum := 0 i := 1 for i <= n { sum = sum + i i = i + 1 } println("worker", id, "=", sum) }
func main() { i := 1 for i <= 5 { go work(i, i * 1000) i = i + 1 } sleep(200) }
```

Here five `work` calls run concurrently. `sleep(ms)` (milliseconds) gives the
goroutines time to finish before `main` returns. See
[`examples/complex/goroutines.mfl`](../examples/complex/goroutines.mfl).

## Walkthrough: a concurrent HTTP server

The flagship example accepts connections in a loop and hands each one to a
goroutine, so slow clients never block new ones. Decoded, the program reads:

```go
func page() {
    return "<!doctype html><html><body><h1>Hello from MFL</h1></body></html>"
}

func response() {
    body := page()
    return "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: " + str(len(body)) + "\r\nConnection: close\r\n\r\n" + body
}

func handle(conn) {
    read(conn)              // consume the request
    write(conn, response()) // send the response
    close(conn)             // done with this client
}

func main() {
    server := listen(48080)
    println("MFL http server on http://localhost:48080")
    for {                   // accept forever
        conn := accept(server)
        go handle(conn)     // serve this client concurrently
    }
}
```

Step by step:

1. **`listen(48080)`** binds the listener once and returns the server handle.
2. **`for { ... }`** is a bare infinite loop — the accept loop.
3. **`accept(server)`** blocks until a browser connects, returning that
   connection.
4. **`go handle(conn)`** serves the connection on its own goroutine, so the loop
   immediately goes back to `accept` for the next client.
5. **`handle`** reads the request, builds an HTTP response whose
   `Content-Length` is computed with `str(len(body))`, writes it, and closes.

### Running it

```sh
machin run examples/complex/http_server.mfl
# then, in another terminal:
curl http://localhost:48080
```

Because the body length is computed from the actual page (`len(body)`), the
`Content-Length` header always matches the payload, and `Connection: close`
tells the client the response is complete.
