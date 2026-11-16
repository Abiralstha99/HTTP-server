//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"time"
)

// runEventLoop owns client sockets. The caller owns the listener and queue.
func runEventLoop(listenerFD, queueFD int, root string) error {
	clients := make(map[int]*Client)
	defer func() {
		for clientFD, client := range clients {
			if err := closeClient(clientFD, client, clients); err != nil {
				fmt.Println(err)
			}
		}
	}()

	events := make([]syscall.Kevent_t, 64)
	owners := make([]*Client, len(events))
	for {
		// Read deadlines apply while awaiting headers, not while sending output.
		var nextDeadline time.Time
		now := time.Now()
		for clientFD, client := range clients {
			if len(client.output) > 0 {
				continue
			}
			if !client.deadline.After(now) {
				if err := closeClient(clientFD, client, clients); err != nil {
					fmt.Println(err)
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
			owners[i] = clients[int(event.Ident)]
		}
		for i, event := range events[:n] {
			clientFD := int(event.Ident)
			if clientFD == listenerFD {
				if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
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
				err = io.EOF
			} else if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
				err = fmt.Errorf("client event: %w", syscall.Errno(event.Data))
			} else if event.Flags&syscall.EV_EOF != 0 && event.Fflags != 0 {
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
				if !errors.Is(err, io.EOF) {
					fmt.Println(err)
				}
				if err := closeClient(clientFD, client, clients); err != nil {
					fmt.Println(err)
				}
			}
		}
	}
}
