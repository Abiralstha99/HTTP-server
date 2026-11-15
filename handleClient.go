package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type HTTPMessage struct {
	Version string
	Headers map[string]string
	Body    []byte
}

type HTTPRequest struct {
	HTTPMessage
	Method string
	Path   string
}

type HTTPResponse struct {
	HTTPMessage
	StatusCode int
	StatusText string
}

func serializeResponse(response HTTPResponse) []byte {
	message := fmt.Sprintf("HTTP/1.1 %d %s\r\n", response.StatusCode, response.StatusText)
	message += fmt.Sprintf("Content-Length: %d\r\n", len(response.Body))
	for key, value := range response.Headers {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		message += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	message += "\r\n"
	message += string(response.Body)
	return []byte(message)
}

func WriteResponse(conn net.Conn, response HTTPResponse) error {
	message := serializeResponse(response)
	n, err := conn.Write(message)
	if err != nil {
		return err
	}
	if n != len(message) {
		return io.ErrShortWrite
	}
	return nil
}

const maxHeaderBytes = 8192
const readTimeout = 30 * time.Second

func ReadRequest(reader *bufio.Reader) (*HTTPRequest, error) {
	var data []byte

	// Read at most one header block, without consuming the next request.
	for len(data) < maxHeaderBytes {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		data = append(data, b)

		if bytes.HasSuffix(data, []byte("\r\n\r\n")) {
			headerData := strings.TrimSuffix(string(data), "\r\n\r\n")
			return parseRequest(headerData)
		}
	}

	return nil, fmt.Errorf("headers too large")
}

func parseRequest(headerData string) (*HTTPRequest, error) {
	lines := strings.Split(headerData, "\r\n")

	// Parse request line: GET /echo/hello HTTP/1.1
	parts := strings.Fields(lines[0])

	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid HTTP request line")
	}

	// Parse headers
	headers := make(map[string]string)

	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("invalid HTTP header")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if _, exists := headers[key]; exists && key == "content-length" {
			return nil, fmt.Errorf("duplicate content-length")
		}

		if old, exists := headers[key]; exists && key == "connection" {
			headers[key] = old + "," + value
		} else {
			headers[key] = value
		}
	}

	return &HTTPRequest{
		HTTPMessage: HTTPMessage{
			Version: parts[2],
			Headers: headers,
			Body:    []byte{},
		},
		Method: parts[0],
		Path:   parts[1],
	}, nil
}

func routeRequest(httpReq HTTPRequest, root string) HTTPResponse {
	if strings.HasPrefix(httpReq.Path, "/echo") {
		echo := strings.TrimPrefix(httpReq.Path, "/echo")
		body := []byte(echo)
		return HTTPResponse{
			StatusCode: 200,
			StatusText: "OK",
			HTTPMessage: HTTPMessage{
				Version: "HTTP/1.1",
				Headers: map[string]string{
					"Content-Type": "text/plain",
				},
				Body: body,
			},
		}
	}

	path := httpReq.Path
	if path == "/" {
		path = "/index.html"
	}

	file_path := filepath.Join(root, path)

	_, err := os.Stat(file_path)
	if err != nil {
		return HTTPResponse{
			StatusCode: 404,
			StatusText: "Not Found",
			Headers: map[string]string{
				"Content-Type": "text/plain",
			},
			Body: []byte("404 Not Found"),
		}
	}

	content, err := os.ReadFile(file_path)
	if err != nil {
		return HTTPResponse{
			StatusCode: 400,
			StatusText: "Bad Request",
			Headers: map[string]string{
				"Content-Type": "text/plain",
			},
			Body: []byte("400 Bad Request"),
		}
	}

	return HTTPResponse{
		StatusCode: 200,
		StatusText: "OK",
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		Body: content,
	}
}

func shouldClose(httpReq *HTTPRequest) bool {
	// HTTP/1.0 always closes.
	if httpReq.Version != "HTTP/1.1" {
		return true
	}

	// HTTP/1.1 closes only when a Connection token is "close".
	for _, token := range strings.Split(httpReq.Headers["connection"], ",") {
		if strings.EqualFold(strings.TrimSpace(token), "close") {
			return true
		}
	}
	return false
}

// validateRequest returns an error status, or zero for a supported request.
func validateRequest(httpReq HTTPRequest) (int, string) {
	if _, exists := httpReq.Headers["transfer-encoding"]; exists {
		return 400, "Bad Request"
	}
	if length, exists := httpReq.Headers["content-length"]; exists {
		if length == "" || strings.Trim(length, "0") != "" {
			return 400, "Bad Request"
		}
	}
	if httpReq.Method != "GET" {
		return 405, "Method Not Allowed"
	}
	return 0, ""
}

func handleClient(conn net.Conn, id int, root string) {
	defer conn.Close()

	start := time.Now()
	fmt.Printf("Connection handler id: %d\n", id)

	reader := bufio.NewReader(conn)

	for {
		// SetReadDeadline sets the deadline for future Read calls
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			fmt.Println("Error setting read deadline:", err)
			break
		}

		// 1. Read one complete HTTP request
		httpReq, err := ReadRequest(reader)

		// 2. Handle errors or client disconnects
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			// io.EOF means the other side has closed its sending side and there's no more data to read.
			if !errors.Is(err, io.EOF) {
				if err := WriteResponse(conn, HTTPResponse{
					StatusCode: 400,
					StatusText: "Bad Request",
					Headers: map[string]string{
						"Content-Type": "text/plain",
						"Connection":   "close",
					},
					Body: []byte("400 Bad Request"),
				}); err != nil {
					fmt.Println("Error writing response:", err)
				}
			}
			break
		}

		// Reject bodies and transfer encodings before routing, including on GET.
		status, statusText := validateRequest(*httpReq)

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
			response = routeRequest(*httpReq, root)
		}

		// Decide to close → Tell client → Send response → Close
		closeConn := response.StatusCode >= 400 || shouldClose(httpReq)
		if closeConn {
			if response.Headers == nil {
				response.Headers = make(map[string]string)
			}
			response.Headers["Connection"] = "close"
		}

		if err := WriteResponse(conn, response); err != nil {
			fmt.Println("Error writing response:", err)
			break
		}

		if closeConn {
			break
		}
	}

	fmt.Printf("Connection handler finished for id: %d | duration: %v\n", id, time.Since(start))
}
