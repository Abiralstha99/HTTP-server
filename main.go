package main

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

func main() {
	start := time.Now()

	root := "www"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	var wg sync.WaitGroup

	// Create a TCP listener on port and wait for a connection
	listener, err := net.Listen("tcp", "127.0.0.1:8000")

	if err != nil {
		fmt.Println("Failed to bind to port 80")
		os.Exit(1)
	}

	fmt.Println("Server is listening on port 8080")
	fmt.Println("Serving from:", root)

	id := 0
	for {
		// Block until we receive an incoming connection
		conn, err := listener.Accept()
		if err != nil {
			// Listener closed during shutdown
			break
		}
		id++
		wg.Add(1)
		// Handle client connection using a goroutine
		go func(conn net.Conn, id int) {
			defer wg.Done()
			handleClient(conn, id, root)
		}(conn, id)
	}
	wg.Wait()
	fmt.Println("Total time taken:", time.Since(start))
}
