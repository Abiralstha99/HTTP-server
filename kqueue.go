package main

import (
	"fmt"
	"syscall"
)

// createEventQueue registers the listener for read readiness.
// The caller must close the returned queue with syscall.Close.
func createEventQueue(listenerFD int) (int, error) {
	queueFD, err := syscall.Kqueue()
	if err != nil {
		return -1, fmt.Errorf("create kqueue: %w", err)
	}
	syscall.CloseOnExec(queueFD)

	var event syscall.Kevent_t

	// EVFILT_READ: Notify me when a socket is ready for a read-related operation.
	syscall.SetKevent(&event, listenerFD, syscall.EVFILT_READ, syscall.EV_ADD|syscall.EV_ENABLE)
	if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{event}, nil, nil); err != nil {
		syscall.Close(queueFD)
		return -1, fmt.Errorf("register listener: %w", err)
	}
	return queueFD, nil
}

// waitForEvents fills events and returns the number of entries to process.
// Process ready sockets before waiting again; readiness is level-triggered.
func waitForEvents(queueFD int, events []syscall.Kevent_t) (int, error) {
	if len(events) == 0 {
		return 0, fmt.Errorf("event buffer is empty")
	}
	for {
		n, err := syscall.Kevent(queueFD, nil, events, nil)
		// EINTR: The system call was interrupted by a signal.
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("wait for events: %w", err)
		}
		for _, event := range events[:n] {
			// EV_ERROR: The event has an error condition.
			if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
				return 0, fmt.Errorf("kqueue event: %w", syscall.Errno(event.Data))
			}
		}
		return n, nil
	}
}
