//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

// Each worker owns its clients and connection handoffs.
type Worker struct {
	clients map[int]*Client
	control workerControl
}

func newWorker() *Worker {
	return &Worker{clients: make(map[int]*Client)}
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
						if err := worker.registerClient(command.fd, queueFD, root, command.deadline); err != nil {
							for _, pending := range commands[i+1:] {
								if pending.kind == "connection" {
									syscall.Close(pending.fd)
								}
							}
							return err
						}
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
					err = worker.handleClientRead(clientFD, client)
				case syscall.EVFILT_WRITE:
					err = worker.handleClientWrite(clientFD, client)
				default:
					err = nil
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					fmt.Fprintf(os.Stderr, "Client %d error: %v\n", clientFD, err)
				}
				if err := closeClient(clientFD, client, worker.clients); err != nil {
					return err
				}
			}
		}
	}
}
