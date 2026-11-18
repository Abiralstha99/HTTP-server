//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Only the event loop and its synchronous helpers update these counters.
var counters = struct {
	normalEOF                 uint64
	readDeadlineExpired       uint64
	protocolRejection         uint64
	readErrors                map[syscall.Errno]uint64
	writeErrors               map[syscall.Errno]uint64
	socketEventErrors         map[syscall.Errno]uint64
	shortWrites               uint64
	kqueueRegistrationFailure uint64
}{readErrors: make(map[syscall.Errno]uint64), writeErrors: make(map[syscall.Errno]uint64), socketEventErrors: make(map[syscall.Errno]uint64)}

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

func printCounters(kind string) {
	fmt.Printf("Counters %s: normal_eof=%d read_deadline_expired=%d protocol_rejection=%d read_errors=%s write_errors=%s socket_event_errors=%s short_writes=%d kqueue_registration_failure=%d\n", kind,
		counters.normalEOF, counters.readDeadlineExpired, counters.protocolRejection, errorCounts(counters.readErrors), errorCounts(counters.writeErrors), errorCounts(counters.socketEventErrors), counters.shortWrites, counters.kqueueRegistrationFailure)
}

// runEventLoop owns client sockets. The caller owns the listener and queue.
func runEventLoop(listenerFD, queueFD int, root string) (loopErr error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	defer signal.Stop(signals)
	var signalEvent syscall.Kevent_t
	syscall.SetKevent(&signalEvent, int(syscall.SIGUSR1), syscall.EVFILT_SIGNAL, syscall.EV_ADD|syscall.EV_ENABLE)
	if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{signalEvent}, nil, nil); err != nil {
		counters.kqueueRegistrationFailure++
		return fmt.Errorf("register snapshot signal: %w", err)
	}
	clients := make(map[int]*Client)
	defer func() {
		for clientFD, client := range clients {
			if err := closeClient(clientFD, client, clients); err != nil {
				loopErr = errors.Join(loopErr, err)
			}
		}
	}()

	events := make([]syscall.Kevent_t, 64)
	owners := make([]*Client, len(events))
	nextSummary := time.Now().Add(30 * time.Second)
	for {
		// Read deadlines apply while awaiting headers, not while sending output.
		var nextDeadline time.Time
		now := time.Now()
		for clientFD, client := range clients {
			if len(client.output) > 0 {
				continue
			}
			if !client.deadline.After(now) {
				counters.readDeadlineExpired++
				if err := closeClient(clientFD, client, clients); err != nil {
					return err
				}
				continue
			}
			if nextDeadline.IsZero() || client.deadline.Before(nextDeadline) {
				nextDeadline = client.deadline
			}
		}
		if !nextSummary.After(now) {
			printCounters("periodic")
			nextSummary = now.Add(30 * time.Second)
		}
		if nextDeadline.IsZero() || nextSummary.Before(nextDeadline) {
			nextDeadline = nextSummary
		}

		n, err := waitForEvents(queueFD, events, nextDeadline)
		if err != nil {
			return err
		}
		// Keep each event associated with its original client, even if an accept
		// later reuses a descriptor closed earlier in this same batch.
		for i, event := range events[:n] {
			owners[i] = clients[int(event.Ident)]
		}
		for i, event := range events[:n] {
			if event.Filter == syscall.EVFILT_SIGNAL && event.Ident == uint64(syscall.SIGUSR1) {
				if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
					return fmt.Errorf("snapshot signal event: %w", syscall.Errno(event.Data))
				}
				printCounters("snapshot")
				continue
			}
			clientFD := int(event.Ident)
			if clientFD == listenerFD {
				if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
					counters.kqueueRegistrationFailure++
					return fmt.Errorf("listener event: %w", syscall.Errno(event.Data))
				}
				if event.Flags&syscall.EV_EOF != 0 {
					return fmt.Errorf("listener closed")
				}
				if err := acceptClients(listenerFD, queueFD, clients, root); err != nil {
					return err
				}
				continue
			}
			client := owners[i]
			if client == nil || client.closed || clients[clientFD] != client {
				continue
			}
			if len(client.output) == 0 && !client.deadline.After(time.Now()) {
				counters.readDeadlineExpired++
				err = io.EOF
			} else if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
				counters.kqueueRegistrationFailure++
				err = fmt.Errorf("client event: %w", syscall.Errno(event.Data))
			} else if event.Flags&syscall.EV_EOF != 0 && event.Fflags != 0 {
				counters.socketEventErrors[syscall.Errno(event.Fflags)]++
				err = fmt.Errorf("client socket: %w", syscall.Errno(event.Fflags))
			} else {
				// EV_EOF alone can be a half-close with unread request data.
				// Let the handlers read that data and finish queued responses.
				switch event.Filter {
				case syscall.EVFILT_READ:
					err = handleClientRead(clientFD, client)
				case syscall.EVFILT_WRITE:
					err = handleClientWrite(clientFD, client)
				default:
					err = nil
				}
			}
			if err != nil {
				if err := closeClient(clientFD, client, clients); err != nil {
					return err
				}
			}
		}
	}
}
