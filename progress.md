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

## 2026-11-15 — Prepare kqueue event waiting

`kqueue_darwin.go` creates a queue and registers the listener for level-triggered read readiness. Its batch wait retries interrupted waits and reports wait or event errors. Registration failure closes the queue; successful queues belong to the caller. These helpers are not yet integrated into `main` or connection acceptance.

## 2026-11-15 — Accept and track non-blocking clients

`client.go` drains pending accepts, explicitly makes each accepted socket non-blocking, and registers read readiness before adding it to the caller's client map. Each client stores input/output buffers, a write offset, a 30-second deadline, and closure flags. Failed socket setup closes that socket; tracked sockets remain owned by the caller. Integration with `main`, client I/O, and deadline enforcement remain future steps.

## 2026-11-15 — Assemble requests on non-blocking clients

`handleClientRead` reads within the 8192-byte header limit, preserves incomplete headers and following requests, and queues one serialized response using the existing HTTP helpers. It preserves pending output and marks error or close responses for closure after writing. Client state now uses the `Client` type and stores the configured document root. Buffered requests must be processed again after output is cleared; writing, deadline enforcement, and integration with `main` remain future steps.

## 2026-11-15 — Write queued responses incrementally

Clients enable write readiness when queuing output and retain the write offset across partial writes or would-block results. Drained output disables write readiness before processing another buffered request. Responses marked for closure return `io.EOF` only after all output is sent, leaving socket cleanup to the caller. These handlers remain preparation for integration with the main event loop.

## 2026-11-15 — Complete the keep-alive readiness handoff

Read readiness is paused while a response is pending. Once output drains, closure rules are applied first; a persistent client resumes read readiness with a fresh deadline and immediately processes its next buffered request, if present. One response remains queued at a time to preserve order. Socket cleanup on `io.EOF` and integration with `main` remain caller responsibilities.

## 2026-11-15 — Enforce deadlines and centralize event-loop cleanup

`eventLoop.go` coordinates timed waits, accepts, client events, and cleanup without replacing the current `main`. Read deadlines expire idle or incomplete headers after 30 seconds, including when header fragments arrive. Client errors are isolated, half-closes preserve response order, and event batches retain client identity across descriptor reuse. Cleanup removes and closes a client exactly once and releases its buffers.

Validation: `go build` passed. An isolated loop on an ephemeral local port closed idle and fragmented-header clients at 30.00 seconds, served two pipelined requests after a half-close, rejected a truncated half-closed header, and survived 250 alternating reset/reconnect checks. Separate socket checks confirmed repeated cleanup and stale cleanup do not close a replacement descriptor. Interrupted-wait handling was reviewed but was not forced by the socket checks. No new load benchmark was run; write timeouts and `main` integration remain future steps.

## 2026-11-15 — Switch startup to the event loop

`main.go` now starts the Darwin event loop through platform-specific startup functions, preserving the default `www` root and first-argument override. Listener and queue setup are cleaned up on returned failures. Darwin build tags isolate kqueue code; other platforms compile an explicit unsupported-platform startup error. Startup logs now identify the actual listener address.

Validation: native macOS build and Linux/amd64 and Windows/amd64 cross-builds passed. The production binary reported an occupied-port bind error and exited with status 1. An isolated copy, changing only the listener port and its startup message to 18080 because another server occupied 8000, passed default-root and custom-root static serving, ordered pipelining, sequential reuse, close responses, and unsupported-method rejection. Linux and Windows binaries were not executed. No new benchmark was run; blocking file reads, absent write timeouts, and signal-driven graceful shutdown remain limitations.

## 2026-11-15 — Keep Darwin startup in main.go

Merged the Darwin startup function into `main.go` and removed `startupDarwin.go`. The unsupported-platform file supplies its own entry point so non-Darwin builds can still report the platform limitation. Document-root selection, error reporting, and deferred descriptor cleanup are unchanged.

## 2026-11-15 — Event-loop load benchmarks

User-reported results after switching startup to the single Darwin kqueue loop. Both runs used four threads for one minute against `http://127.0.0.1:8000/echo/benchmark`, with `--latency` and connection targets of 500 and 10,000. Output was captured with `tee /tmp/wrk-500.txt` and `tee /tmp/wrk-10000.txt`. Client and server ran on the local machine; descriptor limits, actual simultaneous connections, and repeat-run variability were not supplied.

| Measurement | 500 connections | 10,000 connections |
| --- | ---: | ---: |
| Completed requests | 8,747,299 | 4,332,385 |
| Requests/sec | 145,711.04 | 72,164.28 |
| Data read | 625.66 MB | 309.88 MB |
| Transfer/sec | 10.42 MB | 5.16 MB |
| Average latency | 3.47 ms | 138.34 ms |
| Latency standard deviation | 1.34 ms | 29.00 ms |
| Median latency | 3.36 ms | 136.54 ms |
| 75th percentile | 3.45 ms | 137.65 ms |
| 90th percentile | 3.56 ms | 141.43 ms |
| 99th percentile | 6.48 ms | 240.65 ms |
| Maximum reported latency | 67.87 ms | 1.83 s |
| Socket errors: connect | None reported | 0 |
| Socket errors: read | None reported | 11,120 |
| Socket errors: write | None reported | 507 |
| Socket errors: timeout | None reported | 0 |

Connection policy: only bodyless GET requests are supported, with ordered persistent HTTP/1.1 responses. Errors, HTTP/1.0, and explicit close requests close after pending output is sent. The request-read timeout is 30 seconds, is not extended by fragments, and excludes output writes; no write timeout is implemented.

Compared with the previous one-minute 500-connection run (164,152.64 requests/sec), throughput decreased about 11.2%; average latency increased from 1.88 to 3.47 ms, while p99 fell from 10.67 to 6.48 ms. Neither supplied 500-connection output reported socket errors. Against the latest prior 10,000-connection run (66,415.81 requests/sec), throughput increased about 8.7%, p99 fell from 1.15 s to 240.65 ms, and reported timeouts fell from 5,380 to zero; read errors increased from 2,621 to 11,120 and write errors from 233 to 507. These are single-run comparisons across implementation changes, not proof of causation or reliable service at 10,000 active connections. The socket error causes remain unverified; no source code was changed in response to these measurements.

## 2026-11-17 — Add aggregate diagnostic counters

Added cumulative normal EOF, read deadline expiry, protocol rejection, socket read/write error, and kqueue registration/update failure counters. Counters increment at detection points; server-policy closures, routing errors such as 404, interruptions, and would-block are excluded from their unrelated categories. The single loop prints one summary every 30 seconds, including while idle, instead of printing each client error. Fatal failures still propagate to startup; no signal-driven final summary was added.

Validation: `go build` passed. Isolated socket checks observed exactly one normal EOF, three protocol rejections, two socket errors, and three registration/update failures, while confirming server-policy close, repeated pending-output handler calls, would-block, and 404 did not inflate counts. A live loop on an ephemeral local port printed two cumulative summaries 30 seconds apart; the second showed exactly two read deadline expirations, one EOF, and one protocol rejection, with no per-request lines. No repository test files or dependencies were added, and no new load benchmark was run.

## 2026-11-17 — 10,000-connection benchmark with diagnostic counters

User-reported `wrk -t4 -c10000 -d60s --latency http://127.0.0.1:8000/echo/benchmark`:

- 3,951,478 requests in one minute; 65,818.47 requests/sec; 282.63 MB read; 4.71 MB/sec.
- Latency: average 154.70 ms, standard deviation 67.06 ms, median 139.23 ms, p75 149.14 ms, p90 191.09 ms, p99 462.94 ms, maximum 1.99 s.
- wrk socket errors: connect 0, read 13,828, write 313, timeout 18.
- Supplied cumulative server summaries first showed normal_eof=1 and socket_error=231, then normal_eof=7214 and socket_error=3018. Every supplied summary showed zero read_deadline_expired, protocol_rejection, and kqueue_registration_failure. Each pair of summaries repeated the same values.

The summaries are cumulative from process startup and are not synchronized with wrk's measurement window. They identify server-observed socket failures but do not distinguish operation or errno; the root cause remains unverified. There is no evidence in the supplied counters of server read-deadline or protocol rejection closures. Compared with the prior event-loop run, throughput fell from 72,164.28 requests/sec and p99 increased from 240.65 ms. No server code changed in response to this report.

## 2026-11-17 — Categorized socket errors and benchmark snapshots

Split socket errors into read, write, and kqueue socket-event counts by errno, with a separate count for zero-progress writes. Added SIGUSR1 snapshots handled by the event loop and a wrapper that waits for snapshots before and after wrk. Counters remain cumulative; the difference brackets the benchmark command and may include other traffic or setup and teardown.

Validation: `go build` passed. An isolated copy listening on port 18081 ran a two-second wrk smoke check with two connections; before and after snapshots were captured, and the latter included a socket-event errno 54 (connection reset by peer). Port 8000 was occupied. This short smoke check is not a load benchmark or a diagnosis of prior failures.

## 2026-11-18 — 10,000-connection benchmark with errno snapshots

User-reported one-minute `wrk -t4 -c10000 -d60s --latency` against `/echo/benchmark`: 4,177,129 requests, 69,526.64 requests/sec, 298.77 MB read, and 4.97 MB/sec. Latency averaged 144.87 ms, with median 137.57 ms, p75 142.19 ms, p90 159.70 ms, p99 344.12 ms, and maximum 1.87 s. wrk reported 0 connect errors, 13,310 read errors, 1,860 write errors, and 0 timeouts.

The server snapshots bracketed the wrk command. Their differences were 5,942 normal EOFs and 2,909 kqueue socket-event errors with errno 54 (connection reset by peer); read errors, write errors, short writes, deadline expirations, protocol rejections, and kqueue registration failures remained zero. Server and client counts describe different observations and cannot be equated. The snapshots may include other traffic and benchmark setup or teardown, and the connection count is a target. The source and timing of the resets remain unverified. Compared with the prior reported diagnostic run, throughput rose from 65,818.47 requests/sec and p99 fell from 462.94 ms; these are single-run comparisons.

At a 10,000-connection target, the server achieved approximately 69.5K requests/sec. Server counters recorded 2,909 kqueue-reported connection resets during the benchmark window. A TCP capture confirmed a client-originated reset on an inspected connection, but the underlying cause remains undetermined. Further investigation is deferred while developing the multithreaded event-loop architecture.
