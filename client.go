//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"io"
	"syscall"
	"time"
)

type Client struct {
	queueFD         int       // Queue used to monitor this client's socket
	root            string    // Document root used for static files
	input           []byte    // Stores request bytes received so far
	output          []byte    // Stores response bytes to be sent
	writeOffset     int       // Tracks how many response bytes have already been sent
	deadline        time.Time // Time when the client should time out
	closeAfterWrite bool      // Whether to close the connection after sending its response
	closed          bool      // Whether the connection has been marked as closed
}

// closeClient removes the current owner before closing its descriptor.
// A stale event or repeated cleanup must never close a replacement client.
func closeClient(clientFD int, client *Client, clients map[int]*Client) error {
	if client.closed || clients[clientFD] != client {
		return nil
	}
	client.closed = true
	delete(clients, clientFD)
	client.input = nil
	client.output = nil
	if err := syscall.Close(clientFD); err != nil {
		return fmt.Errorf("close client: %w", err)
	}
	return nil
}

// acceptClients drains pending connections into an initialized clients map.
// The caller owns the tracked sockets and must close them when finished.
func acceptClients(listenerFD, queueFD int, clients map[int]*Client, root string) error {
	for {
		fd, _, err := syscall.Accept(listenerFD)
		if err == syscall.EINTR || err == syscall.ECONNABORTED {
			continue
		}
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil
		}
		if err != nil {
			return fmt.Errorf("accept client: %w", err)
		}
		syscall.CloseOnExec(fd)
		if err := syscall.SetNonblock(fd, true); err != nil {
			syscall.Close(fd)
			return fmt.Errorf("set client non-blocking mode: %w", err)
		}
		var event syscall.Kevent_t
		syscall.SetKevent(&event, fd, syscall.EVFILT_READ, syscall.EV_ADD|syscall.EV_ENABLE)
		if _, err := syscall.Kevent(queueFD, []syscall.Kevent_t{event}, nil, nil); err != nil {
			syscall.Close(fd)
			return fmt.Errorf("register client: %w", err)
		}
		clients[fd] = &Client{queueFD: queueFD, root: root, deadline: time.Now().Add(readTimeout)}
	}
}

// handleClientRead queues one response. Call it again after that response is sent,
// even if there is no new read event, to process any buffered next request.
// io.EOF means a clean disconnect; other I/O errors also require caller cleanup.
func handleClientRead(clientFD int, client *Client) error {
	if client.closed || client.closeAfterWrite || len(client.output) > 0 {
		return nil
	}

	var buffer [4096]byte
	headerEnd := -1
	malformed := false
	for {
		if end := bytes.Index(client.input, []byte("\r\n\r\n")); end >= 0 {
			headerEnd = end + 4
			malformed = headerEnd > maxHeaderBytes
			break
		}
		if len(client.input) >= maxHeaderBytes {
			malformed = true
			break
		}

		// Bound the read itself, including when the peer never sends a newline.
		remaining := maxHeaderBytes - len(client.input)
		if remaining > len(buffer) {
			remaining = len(buffer)
		}
		n, err := syscall.Read(clientFD, buffer[:remaining])
		if n > 0 {
			client.input = append(client.input, buffer[:n]...)
		}
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read client: %w", err)
		}
		if n == 0 {
			if len(client.input) == 0 {
				return io.EOF
			}
			malformed = true
			break
		}
	}

	status, statusText := 400, "Bad Request"
	var request *HTTPRequest
	if !malformed {
		var err error
		request, err = parseRequest(string(client.input[:headerEnd-4]))
		if err == nil {
			status, statusText = validateRequest(*request)
		}
	}

	var response HTTPResponse
	if status != 0 {
		response = HTTPResponse{
			StatusCode: status,
			StatusText: statusText,
			Headers:    map[string]string{"Content-Type": "text/plain"},
			Body:       fmt.Appendf(nil, "%d %s", status, statusText),
		}
		if status == 405 {
			response.Headers["Allow"] = "GET"
		}
	} else {
		response = routeRequest(*request, client.root)
	}
	client.closeAfterWrite = response.StatusCode >= 400 || shouldClose(request)
	if client.closeAfterWrite {
		response.Headers["Connection"] = "close"
		client.input = nil
	} else {
		client.input = client.input[headerEnd:]
	}
	client.output = serializeResponse(response)
	client.writeOffset = 0
	if err := setClientReadiness(clientFD, client, syscall.EVFILT_READ, false); err != nil {
		return err
	}
	return setClientReadiness(clientFD, client, syscall.EVFILT_WRITE, true)
}

func setClientReadiness(clientFD int, client *Client, filter int, enabled bool) error {
	flags := syscall.EV_DISABLE
	if enabled {
		flags = syscall.EV_ADD | syscall.EV_ENABLE
	}
	var event syscall.Kevent_t
	syscall.SetKevent(&event, clientFD, filter, flags)
	for {
		_, err := syscall.Kevent(client.queueFD, []syscall.Kevent_t{event}, nil, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("update client readiness: %w", err)
		}
		return nil
	}
}

// handleClientWrite preserves unsent bytes until the socket is writable again.
// io.EOF means the response is drained and the caller should close the client.
func handleClientWrite(clientFD int, client *Client) error {
	if client.closed || len(client.output) == 0 {
		return nil
	}
	for client.writeOffset < len(client.output) {
		n, err := syscall.Write(clientFD, client.output[client.writeOffset:])
		if n > 0 {
			client.writeOffset += n
		}
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil
		}
		if err != nil {
			return fmt.Errorf("write client: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	if err := setClientReadiness(clientFD, client, syscall.EVFILT_WRITE, false); err != nil {
		return err
	}
	client.output = nil
	client.writeOffset = 0
	if client.closeAfterWrite {
		return io.EOF
	}
	client.deadline = time.Now().Add(readTimeout)
	if err := setClientReadiness(clientFD, client, syscall.EVFILT_READ, true); err != nil {
		return err
	}
	return handleClientRead(clientFD, client)
}
