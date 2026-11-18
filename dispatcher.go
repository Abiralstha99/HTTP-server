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

func (worker *Worker) copyCounters() workerCounters {
	copy := worker.counters
	worker.control.mutex.Lock()
	copy.handoffHighWater = worker.control.highWater
	worker.control.mutex.Unlock()
	copy.readErrors = make(map[syscall.Errno]uint64)
	copy.writeErrors = make(map[syscall.Errno]uint64)
	copy.socketEventErrors = make(map[syscall.Errno]uint64)
	for key, value := range worker.counters.readErrors {
		copy.readErrors[key] = value
	}
	for key, value := range worker.counters.writeErrors {
		copy.writeErrors[key] = value
	}
	for key, value := range worker.counters.socketEventErrors {
		copy.socketEventErrors[key] = value
	}
	return copy
}

func aggregateSnapshots(workers []*Worker, results <-chan workerResult, kind string, dispatchRejection uint64) error {
	replies := make([]chan workerCounters, len(workers))
	for i, worker := range workers {
		replies[i] = make(chan workerCounters, 1)
		if err := worker.enqueue(workerCommand{kind: "snapshot", reply: replies[i]}); err != nil {
			return err
		}
	}
	total := newWorker()
	highWater := make([]int, 0, len(workers))
	for _, reply := range replies {
		var c workerCounters
		select {
		case c = <-reply:
		case result := <-results:
			return fmt.Errorf("worker %d exited during snapshot: %v", result.index, result.err)
		}
		highWater = append(highWater, c.handoffHighWater)
		total.counters.normalEOF += c.normalEOF
		total.counters.readDeadlineExpired += c.readDeadlineExpired
		total.counters.protocolRejection += c.protocolRejection
		total.counters.shortWrites += c.shortWrites
		total.counters.kqueueRegistrationFailure += c.kqueueRegistrationFailure
		for key, value := range c.readErrors {
			total.counters.readErrors[key] += value
		}
		for key, value := range c.writeErrors {
			total.counters.writeErrors[key] += value
		}
		for key, value := range c.socketEventErrors {
			total.counters.socketEventErrors[key] += value
		}
	}
	total.printCounters(kind, dispatchRejection, highWater)
	return nil
}

// Drain in groups of 64 attempts, polling controls between groups until would-block.
// Attempts include EINTR and ECONNABORTED, so those errors cannot starve controls.
func dispatch(listenerFD, queueFD, failureFD int, workers []*Worker, results <-chan workerResult) error {
	events := make([]syscall.Kevent_t, 64)
	nextWorker := 0
	var dispatchRejection uint64
	draining := false
	nextSummary := time.Now().Add(30 * time.Second)
	for {
		deadline := nextSummary
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
				if event.Ident == uint64(syscall.SIGUSR1) {
					if err := aggregateSnapshots(workers, results, "snapshot", dispatchRejection); err != nil {
						return err
					}
				} else {
					return nil
				}
				continue
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
		if !time.Now().Before(nextSummary) {
			if err := aggregateSnapshots(workers, results, "periodic", dispatchRejection); err != nil {
				return err
			}
			nextSummary = time.Now().Add(30 * time.Second)
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
					dispatchRejection++
					continue
				}
				return err
			}
		}
	}
}

func registerSignals(queueFD int) (chan os.Signal, error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGINT, syscall.SIGTERM)
	for _, sig := range []syscall.Signal{syscall.SIGUSR1, syscall.SIGINT, syscall.SIGTERM} {
		var event syscall.Kevent_t
		syscall.SetKevent(&event, int(sig), syscall.EVFILT_SIGNAL, syscall.EV_ADD|syscall.EV_ENABLE)
		if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{event}, nil, nil); err != nil {
			signal.Stop(signals)
			return nil, err
		}
	}
	return signals, nil
}
