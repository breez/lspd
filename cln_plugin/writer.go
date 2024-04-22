package cln_plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type writer struct {
	mtx sync.Mutex
	out io.Writer
}

func newWriter(out io.Writer) *writer {
	return &writer{
		out: out,
	}
}

func (w *writer) Write(msg interface{}) error {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal to json: %w", err)
	}

	data = append(data, TwoNewLines...)
	_, err = w.out.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to rpc socket: %w", err)
	}

	return nil
}
