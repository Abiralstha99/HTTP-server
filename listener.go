//go:build darwin

package main

import (
	"fmt"
	"syscall"
)

// createListener returns a non-blocking socket. The caller must close it.
func createListener() (int, error) {

	// file descriptor as an ID number that the operating system gives to a resource.
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return -1, fmt.Errorf("create socket: %w", err)
	}

	// CloseOnExec means if this process executes a new program, automatically close this socket in the new program.
	syscall.CloseOnExec(fd)

	// SO_REUSEADDR means that the socket can be reused immediately after closing, without waiting for the TIME_WAIT period.
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("set address reuse: %w", err)
	}

	// SetNonblock means that the socket can be used in non-blocking mode.
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("set non-blocking mode: %w", err)
	}

	// Bind the socket to the address and port.
	address := &syscall.SockaddrInet4{Port: 8000, Addr: [4]byte{127, 0, 0, 1}}
	if err := syscall.Bind(fd, address); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("bind socket: %w", err)
	}

	// Listen on the socket for incoming connections.
	if err := syscall.Listen(fd, syscall.SOMAXCONN); err != nil {
		syscall.Close(fd)
		return -1, fmt.Errorf("listen on socket: %w", err)
	}
	return fd, nil
}
