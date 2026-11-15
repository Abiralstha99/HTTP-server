# Project Progress

This file records meaningful milestones and measurements as the project evolves. New entries should preserve the existing history.

## 2026-11-13 — Initial load benchmark

Baseline reported by the user for the current Go server, using `wrk` against `http://127.0.0.1:8000/echo/benchmark`. The server and load generator ran locally. Each run lasted one minute with four `wrk` threads. The requested connection count is a target, not proof that that many connections were active simultaneously.

| Measurement              | 500 connections | 10,000 connections |
| ------------------------ | --------------: | -----------------: |
| Completed requests       |         399,462 |            535,938 |
| Requests/sec             |        6,651.39 |           8,920.04 |
| Data read                |        28.57 MB |           38.33 MB |
| Transfer/sec             |       487.16 KB |          653.32 KB |
| Average latency          |        73.44 ms |          277.53 ms |
| Median latency           |         8.59 ms |           86.41 ms |
| 75th percentile          |        10.23 ms |          428.01 ms |
| 90th percentile          |       191.23 ms |          701.49 ms |
| 99th percentile          |          1.15 s |             1.69 s |
| Maximum reported latency |          2.00 s |             2.00 s |
| Socket errors: connect   |             225 |              4,293 |
| Socket errors: read      |         408,388 |            609,906 |
| Socket errors: write     |           7,051 |              3,389 |
| Socket errors: timeout   |             979 |             24,904 |

Commands:

```sh
wrk -t4 -c500 -d60s --latency http://127.0.0.1:8000/echo/benchmark
wrk -t4 -c10000 -d60s --latency http://127.0.0.1:8000/echo/benchmark
```

The large numbers of socket errors and timeouts make this an unreliable measure of successful service under the requested load. The current handler closes each connection after one response; the impact of that behavior on `wrk` and the causes of the individual errors have not yet been verified. The server and load generator sharing a machine can also affect the results. Do not treat these results as proof of 10,000 simultaneous active clients.

## 2026-11-14 — Preserve unread bytes across ReadRequest calls

`handleClient` keeps a connection-scoped `pending` buffer. `ReadRequest(conn, &pending)` consumes one header block at `\r\n\r\n` and leaves trailing bytes for the next request. The 8192-byte limit applies only to the incomplete current request, not to already-buffered following requests.

## 2026-11-14 — Frame every response

`WriteResponse` sets `Content-Length` from the response body's byte length for every route and error response, allowing clients to identify response boundaries on persistent connections.

## 2026-11-14 — Bound header reads

`ReadRequest` stops reading an incomplete header block at 8192 bytes, including when a line has no newline. Bytes buffered after a complete request remain available for the next request.

## 2026-11-14 — Persistent-connection benchmark

User-reported `wrk` results against `/echo/benchmark`, with four threads and one minute per run, using the same commands as the initial baseline. Connection counts are targets, not verified simultaneous active counts; server and client ran locally.

| Measurement | 500 connections | 10,000 connections |
| --- | ---: | ---: |
| Completed requests | 1,753,231 | 4,513,904 |
| Requests/sec | 29,195.09 | 75,151.47 |
| Data read | 125.40 MB | 322.86 MB |
| Transfer/sec | 2.09 MB | 5.38 MB |
| Average latency | 95.14 ms | 185.57 ms |
| Median latency | 3.93 ms | 51.28 ms |
| 75th percentile | 116.53 ms | 253.59 ms |
| 90th percentile | 278.39 ms | 473.60 ms |
| 99th percentile | 898.99 ms | 1.11 s |
| Maximum reported latency | 2.00 s | 2.00 s |
| Socket errors: connect | 0 | 0 |
| Socket errors: read | 0 | 2,659 |
| Socket errors: write | 0 | 226 |
| Socket errors: timeout | 178 | 4,811 |

Throughput increased approximately 4.39× at 500 connections and 8.42× at 10,000 versus the initial runs. Read and write errors dropped substantially, but timeouts remain. The 500-connection average, 75th and 90th percentile latencies increased despite lower median and 99th percentile latencies. Results are single runs on a shared local machine and do not establish a cause for the remaining errors or prove 10,000 concurrent active connections.

## 2026-11-14 — 500-connection, 30-second benchmark

User-reported run: `wrk -t4 -c500 -d30s --latency http://127.0.0.1:8000/echo/benchmark`.

- 4,972,837 requests in 30.10 seconds; 165,212.38 requests/sec; 355.69 MB read; 11.82 MB/sec.
- Latency: average 2.58 ms, median 1.35 ms, 75th percentile 1.96 ms, 90th percentile 3.63 ms, 99th percentile 23.97 ms, maximum 347.33 ms.
- No socket errors or timeouts were reported in the supplied output.

Throughput was approximately 5.66× the previous 500-connection run and 24.84× the original baseline. This run lasted 30 seconds rather than one minute, so it does not establish the same sustained behavior; the exact cause of the improvement is unverified. It says nothing about the remaining errors at 10,000 connections.

## 2026-11-14 — Repeat 10,000-connection benchmark

User-reported run: `wrk -t4 -c10000 -d60s --latency http://127.0.0.1:8000/echo/benchmark`.

- 3,991,702 requests in one minute; 66,415.81 requests/sec; 285.51 MB read; 4.75 MB/sec.
- Latency: average 199.62 ms, median 60.48 ms, 75th percentile 275.51 ms, 90th percentile 508.49 ms, 99th percentile 1.15 s, maximum 2.00 s.
- Socket errors: connect 0, read 2,621, write 233, timeout 5,380.

The previous 10,000-connection run achieved 75,151.47 requests/sec, with 2,659 read errors, 226 write errors and 4,811 timeouts. Errors persist at this target concurrency; their causes and the number of connections actually active are unverified. Both client and server ran locally, so load-generator and system limits may influence the result.

## 2026-11-14 — 1,000-connection benchmark

User-reported run: `wrk -t4 -c1000 -d60s --latency http://127.0.0.1:8000/echo/benchmark`.

- 9,051,861 requests in one minute; 150,635.80 requests/sec; 647.44 MB read; 10.77 MB/sec.
- Latency: average 4.34 ms, median 2.77 ms, 75th percentile 4.34 ms, 90th percentile 7.75 ms, 99th percentile 28.33 ms, maximum 289.89 ms.
- Socket errors: connect 0, read 1,009, write 316, timeout 0.

This run shows read and write errors at the 1,000-connection target despite no reported timeouts; their cause is unverified. The earlier error-free 500-connection run lasted 30 seconds, so duration differs.

## 2026-11-14 — Repeat 500-connection, one-minute benchmark

User-reported run: `wrk -t4 -c500 -d60s --latency http://127.0.0.1:8000/echo/benchmark`.

- 9,862,615 requests in one minute; 164,152.64 requests/sec; 705.43 MB read; 11.74 MB/sec.
- Latency: average 1.88 ms, median 1.28 ms, 75th percentile 1.78 ms, 90th percentile 3.21 ms, 99th percentile 10.67 ms, maximum 88.28 ms.
- No socket errors or timeouts were reported in the supplied output.

This run and the 1,000-connection run used the same duration. The former reported no socket errors; the latter reported read and write errors. These single runs do not establish the threshold or cause.

## 2026-11-14 — Limit connection read time

Each request has a 30-second read deadline, refreshed before the next request on a persistent connection. Idle connections and incomplete headers are closed when the deadline expires.

## 2026-11-14 — Reject unsupported request bodies and handle write failures

Only bodyless GET requests are routed. Requests with unsupported methods, transfer encoding, or nonzero, invalid, or duplicate content lengths receive an error and close the connection. Incomplete headers are distinguished from clean disconnects, and failed or incomplete response writes stop the connection loop.

## 2026-11-14 — Separate HTTP rules from socket writes

Response serialization and request validation are standalone functions in `handleClient.go`. The connection loop uses them with the existing routing and close policy; wire behavior is unchanged.

## 2026-11-15 — Prepare a Darwin non-blocking listener

`createListener` in `listener_darwin.go` creates an IPv4 TCP socket, enables address reuse and non-blocking mode, binds `127.0.0.1:8000`, and starts listening. Setup failures close the socket; on success the caller owns it. This helper is preparation for a future event loop and is not yet called by `main`.
