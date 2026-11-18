//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type workerResult struct {
	index int
	err   error
}

// Drain in groups of 64 attempts, polling controls between groups until would-block.
// Attempts include EINTR and ECONNABORTED, so those errors cannot starve controls.
func dispatch(listenerFD, queueFD, failureFD int, workers []*Worker, results <-chan workerResult) error {
	events := make([]syscall.Kevent_t, 64)
	nextWorker := 0
	draining := false
	for {
		var deadline time.Time
		if draining {
			deadline = time.Now()
		}
		n, err := waitForEvents(queueFD, events, deadline)
		if err != nil {
			return err
		}
		for _, event := range events[:n] {
			if event.Filter == syscall.EVFILT_SIGNAL {
				if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
					return fmt.Errorf("signal event: %w", syscall.Errno(event.Data))
				}
				return nil
			}
			if int(event.Ident) == failureFD {
				result := <-results
				return fmt.Errorf("worker %d stopped: %v", result.index, result.err)
			}
			if int(event.Ident) != listenerFD {
				continue
			}
			if event.Flags&syscall.EV_ERROR != 0 && event.Data != 0 {
				return fmt.Errorf("listener event: %w", syscall.Errno(event.Data))
			}
			if event.Flags&syscall.EV_EOF != 0 {
				return fmt.Errorf("listener closed")
			}
			draining = true
		}
		if !draining {
			continue
		}
		for attempts := 0; attempts < 64; attempts++ {
			fd, _, err := syscall.Accept(listenerFD)
			if err == syscall.EINTR || err == syscall.ECONNABORTED {
				continue
			}
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				draining = false
				break
			}
			if err != nil {
				return fmt.Errorf("accept client: %w", err)
			}
			deadline := time.Now().Add(readTimeout)
			syscall.CloseOnExec(fd)
			if err := syscall.SetNonblock(fd, true); err != nil {
				syscall.Close(fd)
				return fmt.Errorf("set client non-blocking mode: %w", err)
			}
			worker := workers[nextWorker]
			nextWorker = (nextWorker + 1) % len(workers)
			// The dispatcher owns fd until enqueue succeeds; the worker owns it thereafter.
			if err := worker.enqueue(workerCommand{kind: "connection", fd: fd, deadline: deadline}); err != nil {
				syscall.Close(fd)
				if err == errHandoffQueueFull {
					continue
				}
				return err
			}
		}
	}
}

func registerSignals(queueFD int) (chan os.Signal, error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		var event syscall.Kevent_t
		syscall.SetKevent(&event, int(sig), syscall.EVFILT_SIGNAL, syscall.EV_ADD|syscall.EV_ENABLE)
		if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{event}, nil, nil); err != nil {
			signal.Stop(signals)
			return nil, err
		}
	}
	return signals, nil
}
