package jsonl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Writer struct {
	path string
	mu   sync.Mutex
}

func NewWriter(path string) *Writer {
	return &Writer{path: path}
}

func (w *Writer) Append(record any) error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("create jsonl dir: %w", err)
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal jsonl record: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open jsonl file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append jsonl record: %w", err)
	}
	return nil
}
