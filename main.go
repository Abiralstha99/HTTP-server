//go:build darwin

package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	root := "www"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	if err := runServer(root); err != nil {
		fmt.Fprintln(os.Stderr, "Server error:", err)
		os.Exit(1)
	}
}

func runServer(root string) error {
	listenerFD, err := createListener()
	if err != nil {
		return err
	}
	defer syscall.Close(listenerFD)

	worker := newWorker()
	queueFD, err := worker.createEventQueue(listenerFD)
	if err != nil {
		return err
	}
	defer syscall.Close(queueFD)

	fmt.Println("Server is listening on 127.0.0.1:8000")
	fmt.Println("Serving from:", root)
	return worker.runEventLoop(listenerFD, queueFD, root)
}
