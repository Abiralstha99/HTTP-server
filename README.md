# HTTP-Server

An HTTP/1.1 server built from scratch in Go using raw TCP sockets and macOS `kqueue`. A learning project for understanding HTTP, nonblocking I/O, and concurrent connection handling without `net/http` or third-party frameworks.

## Features

- Manual HTTP request parsing, routing, and response serialization.
- Echo endpoint and static-file serving from a configurable document root.
- Persistent connections, ordered request pipelining, and partial reads/writes.
- Configurable worker event loops with round-robin dispatch and bounded handoff queues.
- 8 KB header limit, 30-second request-read deadlines, and document-root confinement.

## Run

Requires **macOS** and **Go 1.27.1 or later**. From the repository root:

```sh
go build -o server .
./server
```

Listens on `127.0.0.1:8000`, serves `./www`, and uses one worker by default. To change workers or the document root:

```sh
HTTP_WORKERS=4 ./server /absolute/path/to/public
```

Press **Ctrl+C** to stop the server.

## Try it

```sh
curl -i http://127.0.0.1:8000/echo/hello  # Returns /hello
curl -i http://127.0.0.1:8000/            # Serves www/index.html
bash test.sh                            # Sends five concurrent requests
```

Only bodyless GET requests are supported. Missing files return `404`; unsupported methods return `405` when request framing is valid. Static files currently use `Content-Type: text/plain`, including HTML.

## Architecture

```text
TCP listener :8000
        |
Dispatcher (round-robin)
        |
        +--------------+--------------+
        v              v              v
     Worker 1       Worker 2       Worker N
     kqueue         kqueue         kqueue
     clients        clients        clients
```

A dispatcher accepts TCP connections and assigns them round-robin to workers. Each worker runs a `kqueue` event loop in one goroutine, managing multiple sockets and preserving request/response state between readiness events. There is no goroutine per connection.

Each worker's handoff queue holds up to 256 pending connections. The worker owns its assigned sockets and processes requests in order, retaining unsent bytes across partial writes.

## File structure

```text
HTTP-server/
├── main.go           # Configuration, startup, and shutdown
├── listener.go       # Nonblocking TCP listener
├── dispatcher.go     # Accept connections and assign workers
├── workerControl.go  # Bounded handoff queues and wake pipes
├── kqueue.go         # Event registration and timed waits
├── eventLoop.go      # Worker event dispatch and client cleanup
├── client.go         # Client buffers, incremental reads/writes
├── handleClient.go   # HTTP parsing, validation, routing, serialization
├── www/              # Sample static files
├── go.mod            # Go module and version
├── test.sh           # Concurrent curl smoke check
└── benchmark.sh      # wrk command wrapper
```

## Benchmarks

With a four-worker server running, use `wrk` in another terminal:

```sh
bash benchmark.sh -t4 -c10000 -d60s --latency http://127.0.0.1:8000/echo/benchmark
```

Recorded four-worker results using four wrk threads and a 60-second run:

| Target connections | Requests/sec | Average latency | p99 latency | Read / write errors | wrk timeouts |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 10,000 | 65,441.44 | 199.73 ms | 1.11 s | 143 / 1 | 3,766 |

This is a historical user-reported measurement. Target connections do not prove simultaneous active connections, and the errors mean this run does not establish reliable capacity. No new benchmark was run for this README.

## Limitations

This is a limited HTTP implementation: no request bodies, chunked encoding, TLS, or HTTP/2. Static-file reads can block a worker, no write timeout is implemented, and shutdown does not guarantee completion of in-flight responses. High-load errors remain under investigation.

## References

I used these resources for guidance while building this project:

- [A simple HTTP server from scratch — Trung](https://trungams.github.io/2020-08-23-a-simple-http-server-from-scratch/#socket-programming-basics) — socket programming, HTTP messages, and event-driven server design.
- [Final Project: Building a Multithreaded Web Server — The Rust Programming Language](https://doc.rust-lang.org/book/ch21-00-final-project-a-web-server.html) — learning TCP/HTTP and building a web server by hand.
