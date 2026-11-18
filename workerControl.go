//go:build darwin

package main

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
)

const handoffQueueCapacity = 256

var errHandoffQueueFull = errors.New("worker handoff queue full")

type workerCommand struct {
	kind     string
	fd       int
	deadline time.Time
}

type workerControl struct {
	mutex       sync.Mutex
	commands    []workerCommand
	stopped     bool
	readFD      int
	writeFD     int
	connections int
	stopQueued  bool
}

// enqueue transfers ownership of a connection only when it returns nil.
func (worker *Worker) enqueue(command workerCommand) error {
	control := &worker.control
	control.mutex.Lock()
	defer control.mutex.Unlock()
	if control.stopped {
		return fmt.Errorf("worker stopped")
	}
	if command.kind == "connection" && control.connections == handoffQueueCapacity {
		return errHandoffQueueFull
	}
	if command.kind == "stop" && control.stopQueued {
		return fmt.Errorf("worker %s already queued", command.kind)
	}
	if command.kind != "connection" && command.kind != "stop" {
		return fmt.Errorf("unknown worker command: %s", command.kind)
	}
	if control.commands == nil {
		control.commands = make([]workerCommand, 0, handoffQueueCapacity+1)
	}
	control.commands = append(control.commands, command)
	for {
		_, err := syscall.Write(control.writeFD, []byte{1})
		if err == syscall.EINTR {
			continue
		}
		if err != nil && err != syscall.EAGAIN && err != syscall.EWOULDBLOCK {
			control.commands[len(control.commands)-1] = workerCommand{}
			control.commands = control.commands[:len(control.commands)-1]
			return fmt.Errorf("wake worker: %w", err)
		}
		// Commit accounting only after the wake succeeds (or is already pending).
		switch command.kind {
		case "connection":
			control.connections++
		case "stop":
			control.stopQueued = true
		}
		return nil
	}
}

func (worker *Worker) takeCommands() []workerCommand {
	control := &worker.control
	var buffer [256]byte
	for {
		n, err := syscall.Read(control.readFD, buffer[:])
		if err == syscall.EINTR {
			continue
		}
		if n == 0 || err != nil {
			break
		}
	}
	control.mutex.Lock()
	commands := control.commands
	control.commands = nil
	control.connections = 0
	control.stopQueued = false
	control.mutex.Unlock()
	return commands
}

// Closing the write end also wakes kevent if a normal wakeup fails.
func (worker *Worker) requestStop() {
	if worker.enqueue(workerCommand{kind: "stop"}) == nil {
		return
	}
	control := &worker.control
	control.mutex.Lock()
	defer control.mutex.Unlock()
	if !control.stopped && !control.stopQueued {
		control.commands = append(control.commands, workerCommand{kind: "stop"})
		control.stopQueued = true
		syscall.Close(control.writeFD)
		control.writeFD = -1
	}
}

// stopCommands prevents further transfers and closes connections still in the queue.
func (worker *Worker) stopCommands() {
	control := &worker.control
	control.mutex.Lock()
	control.stopped = true
	commands := control.commands
	control.commands = nil
	control.connections = 0
	control.stopQueued = false
	control.mutex.Unlock()
	for _, command := range commands {
		if command.kind == "connection" {
			syscall.Close(command.fd)
		}
	}
}

func createWakePipe(queueFD int) (int, int, error) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		return -1, -1, err
	}
	for _, fd := range fds {
		syscall.CloseOnExec(fd)
		if err := syscall.SetNonblock(fd, true); err != nil {
			syscall.Close(fds[0])
			syscall.Close(fds[1])
			return -1, -1, err
		}
	}
	var event syscall.Kevent_t
	syscall.SetKevent(&event, fds[0], syscall.EVFILT_READ, syscall.EV_ADD|syscall.EV_ENABLE)
	if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{event}, nil, nil); err != nil {
		syscall.Close(fds[0])
		syscall.Close(fds[1])
		return -1, -1, err
	}
	return fds[0], fds[1], nil
}
