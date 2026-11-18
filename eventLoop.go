//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Each worker owns its clients and diagnostic counters.
type workerCounters struct {
	normalEOF                 uint64
	readDeadlineExpired       uint64
	protocolRejection         uint64
	readErrors                map[syscall.Errno]uint64
	writeErrors               map[syscall.Errno]uint64
	socketEventErrors         map[syscall.Errno]uint64
	shortWrites               uint64
	kqueueRegistrationFailure uint64
	handoffHighWater          int
}

type Worker struct {
	clients  map[int]*Client
	counters workerCounters
	control  workerControl
}

func newWorker() *Worker {
	return &Worker{clients: make(map[int]*Client), counters: workerCounters{readErrors: make(map[syscall.Errno]uint64), writeErrors: make(map[syscall.Errno]uint64), socketEventErrors: make(map[syscall.Errno]uint64)}}
}

func errorCounts(counts map[syscall.Errno]uint64) string {
	keys := make([]int, 0, len(counts))
	for errno := range counts {
		keys = append(keys, int(errno))
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d:%s=%d", key, syscall.Errno(key), counts[syscall.Errno(key)]))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (worker *Worker) printCounters(kind string, dispatchRejection uint64, highWater []int) {
	fmt.Printf("Counters %s: normal_eof=%d read_deadline_expired=%d protocol_rejection=%d read_errors=%s write_errors=%s socket_event_errors=%s short_writes=%d kqueue_registration_failure=%d dispatch_rejection=%d handoff_queue_capacity=%d handoff_queue_high_water=%v\n", kind,
		worker.counters.normalEOF, worker.counters.readDeadlineExpired, worker.counters.protocolRejection, errorCounts(worker.counters.readErrors), errorCounts(worker.counters.writeErrors), errorCounts(worker.counters.socketEventErrors), worker.counters.shortWrites, worker.counters.kqueueRegistrationFailure, dispatchRejection, handoffQueueCapacity, highWater)
}

// runEventLoop owns all handed-off sockets. The caller closes its queue and wake pipe.
func (worker *Worker) runEventLoop(queueFD int, root string) (loopErr error) {
	defer worker.stopCommands()
	defer func() {
		for clientFD, client := range worker.clients {
			if err := closeClient(clientFD, client, worker.clients); err != nil {
				loopErr = errors.Join(loopErr, err)
			}
		}
	}()

	events := make([]syscall.Kevent_t, 64)
	owners := make([]*Client, len(events))
	for {
		// Read deadlines apply while awaiting headers, not while sending output.
		var nextDeadline time.Time
		now := time.Now()
		for clientFD, client := range worker.clients {
			if len(client.output) > 0 {
				continue
			}
			if !client.deadline.After(now) {
				worker.counters.readDeadlineExpired++
				if err := closeClient(clientFD, client, worker.clients); err != nil {
					return err
				}
				continue
			}
			if nextDeadline.IsZero() || client.deadline.Before(nextDeadline) {
				nextDeadline = client.deadline
			}
		}

		n, err := waitForEvents(queueFD, events, nextDeadline)
		if err != nil {
			return err
		}
		// Keep each event associated with its original client, even if an accept
		// later reuses a descriptor closed earlier in this same batch.
		for i, event := range events[:n] {
			owners[i] = worker.clients[int(event.Ident)]
		}
		for i, event := range events[:n] {
			clientFD := int(event.Ident)
			if clientFD == worker.control.readFD {
				commands := worker.takeCommands()
				for i, command := range commands {
					switch command.kind {
					case "connection":
						if err := worker.registerClient(command.fd, queueFD, root); err != nil {
							for _, pending := range commands[i+1:] {
								if pending.kind == "connection" {
									syscall.Close(pending.fd)
								}
							}
							return err
						}
					case "snapshot":
						command.reply <- worker.copyCounters()
					case "stop":
						for _, pending := range commands[i+1:] {
							if pending.kind == "connection" {
								syscall.Close(pending.fd)
							}
						}
						return nil
					}
				}
				continue
			}

			client := owners[i]
			if client == nil || client.closed || worker.clients[clientFD] != client {
				continue
			}
			if len(client.output) == 0 && !client.deadline.After(time.Now()) {
				worker.counters.readDeadlineExpired++
				err = io.EOF
			} else if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
				worker.counters.kqueueRegistrationFailure++
				err = fmt.Errorf("client event: %w", syscall.Errno(event.Data))
			} else if event.Flags&syscall.EV_EOF != 0 && event.Fflags != 0 {
				worker.counters.socketEventErrors[syscall.Errno(event.Fflags)]++
				err = fmt.Errorf("client socket: %w", syscall.Errno(event.Fflags))
			} else {
				// EV_EOF alone can be a half-close with unread request data.
				// Let the handlers read that data and finish queued responses.
				switch event.Filter {
				case syscall.EVFILT_READ:
					err = worker.handleClientRead(clientFD, client)
				case syscall.EVFILT_WRITE:
					err = worker.handleClientWrite(clientFD, client)
				default:
					err = nil
				}
			}
			if err != nil {
				if err := closeClient(clientFD, client, worker.clients); err != nil {
					return err
				}
			}
		}
	}
}
