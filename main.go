//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

func main() {
	root := "www"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	workers := 1
	if value, present := os.LookupEnv("HTTP_WORKERS"); present {
		var err error
		workers, err = strconv.Atoi(value)
		if err != nil || workers <= 0 {
			fmt.Fprintln(os.Stderr, "Server error: HTTP_WORKERS must be a positive integer")
			os.Exit(1)
		}
	}
	if err := runServer(root, workers); err != nil {
		fmt.Fprintln(os.Stderr, "Server error:", err)
		os.Exit(1)
	}
}

func runServer(root string, count int) error {
	listenerFD, err := createListener()
	if err != nil {
		return err
	}
	listenerOpen := true
	defer func() {
		if listenerOpen {
			syscall.Close(listenerFD)
		}
	}()

	dispatcher := newWorker()
	queueFD, err := dispatcher.createEventQueue(listenerFD)
	if err != nil {
		return err
	}
	defer syscall.Close(queueFD)
	signals, err := registerSignals(queueFD)
	if err != nil {
		return fmt.Errorf("register signals: %w", err)
	}
	defer signal.Stop(signals)
	failureRead, failureWrite, err := createWakePipe(queueFD)
	if err != nil {
		return fmt.Errorf("create dispatcher wake pipe: %w", err)
	}
	defer syscall.Close(failureRead)
	defer syscall.Close(failureWrite)

	var workers []*Worker
	var queues []int
	defer func() {
		for i, worker := range workers {
			syscall.Close(worker.control.readFD)
			if worker.control.writeFD >= 0 {
				syscall.Close(worker.control.writeFD)
			}
			syscall.Close(queues[i])
		}
	}()
	for i := 0; i < count; i++ {
		worker := newWorker()
		fd, err := syscall.Kqueue()
		if err != nil {
			return fmt.Errorf("create worker queue: %w", err)
		}
		syscall.CloseOnExec(fd)
		readFD, writeFD, err := createWakePipe(fd)
		if err != nil {
			syscall.Close(fd)
			return fmt.Errorf("create worker wake pipe: %w", err)
		}
		worker.control.readFD, worker.control.writeFD = readFD, writeFD
		workers, queues = append(workers, worker), append(queues, fd)
	}
	results := make(chan workerResult, count)
	var running sync.WaitGroup
	running.Add(count)
	for i, worker := range workers {
		go func(index int, worker *Worker, fd int) {
			defer running.Done()
			err := worker.runEventLoop(fd, root)
			results <- workerResult{index, err}
			for {
				_, err := syscall.Write(failureWrite, []byte{1})
				if err != syscall.EINTR {
					break
				}
			}
		}(i, worker, queues[i])
	}
	fmt.Println("Server is listening on 127.0.0.1:8000")
	fmt.Println("Serving from:", root)
	fmt.Println("Workers:", count)
	err = dispatch(listenerFD, queueFD, failureRead, workers, results)
	// Stop accepting before asking workers to close their owned and pending sockets.
	syscall.Close(listenerFD)
	listenerOpen = false
	for _, worker := range workers {
		worker.requestStop()
	}
	// A result may already have been consumed by dispatch, so wait for goroutines separately.
	running.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			err = errors.Join(err, fmt.Errorf("worker %d: %w", result.index, result.err))
		}
	}
	return err
}
