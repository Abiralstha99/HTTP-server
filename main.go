package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

func handleClient(conn net.Conn) {
	// Ensure we close the connection after we're done
	defer conn.Close()

	// Read data
	buf := make([]byte, 1024)
	n_read, err := conn.Read(buf)
	if err != nil {
		return
	}

	req := string(buf[:n_read])
	fmt.Println("Received data", req)

	// Parse the request
	parts := strings.Fields(req)
	fmt.Println("Parts", parts)

	// Parse the request
	method := parts[0]
	url := parts[1]
	version := parts[2]

	fmt.Println("Method", method)
	fmt.Println("URL", url)
	fmt.Println("Version", version)

	// Write the same data back
	message := []byte("HTTP/1.1 200 OK\r\n\r\n " + "Requested path: " + url +"\n")
	n_write, err := conn.Write(message)

	if err != nil {
		fmt.Println("Error writing:", err)
		return
	}
	fmt.Println("Wrote data", (n_write))
}

func main() {
	// Create a TCP listener on port and wait for a connection
	listener, err := net.Listen("tcp", "127.0.0.1:8000")

	if err != nil {
		fmt.Println("Failed to bind to port 80")
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("Server is listening on port 8080")

	for {
		// Block until we receive an incoming connection
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			continue
		}

		// Handle client connection
		handleClient(conn)
	}
}
