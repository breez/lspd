package cln_plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const (
	maxIntakeBuffer = 500 * 1024 * 1023
)

type reader struct {
	in       io.ReadCloser
	mtx      sync.Mutex
	scanner  *bufio.Scanner
	buffered []*Request
}

func newReader(in io.ReadCloser) *reader {
	scanner := bufio.NewScanner(in)
	buf := make([]byte, 1024)
	scanner.Buffer(buf, maxIntakeBuffer)

	// cln messages are split by a double newline.
	scanner.Split(scanDoubleNewline)
	return &reader{
		in:      in,
		scanner: scanner,
	}
}

func (r *reader) Close() error {
	return r.in.Close()
}

func (r *reader) Next() (*Request, error) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	// If there's buffered requests, take that
	if req := r.takeFromBuffer(); req != nil {
		return req, nil
	}

	// Advance the reader
	if !r.scanner.Scan() {
		if r.scanner.Err() != nil {
			return nil, r.scanner.Err()
		} else {
			return nil, io.EOF
		}
	}

	msg := r.scanner.Bytes()
	if len(msg) == 0 {
		return nil, fmt.Errorf("got invalid zero length message")
	}

	// Handle request batches.
	if msg[0] == '[' {
		err := json.Unmarshal(msg, &r.buffered)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal request batch: %w", err)
		}

		return r.takeFromBuffer(), nil
	}

	// Parse the received buffer into a request object.
	var request Request
	err := json.Unmarshal(msg, &request)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}
	return &request, nil
}

func (r *reader) takeFromBuffer() *Request {
	if len(r.buffered) > 0 {
		req := r.buffered[0]
		r.buffered = r.buffered[1:]
		return req
	}

	return nil
}

// Helper method for the bufio scanner to split messages on double newlines.
func scanDoubleNewline(
	data []byte,
	atEOF bool,
) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i+1) < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
	}
	// this trashes anything left over in
	// the buffer if we're at EOF, with no /n/n present
	return 0, nil, nil
}
