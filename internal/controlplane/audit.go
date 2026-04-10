package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/chenan/codo/internal/jsonl"
	"github.com/chenan/codo/internal/protocol"
)

type AuditCollector struct {
	writer  *jsonl.Writer
	mu      sync.Mutex
	pending map[string]protocol.BashStartEvent
}

func NewAuditCollector(logPath string) *AuditCollector {
	return &AuditCollector{
		writer:  jsonl.NewWriter(logPath),
		pending: map[string]protocol.BashStartEvent{},
	}
}

func (c *AuditCollector) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bash/start", c.handleStart)
	mux.HandleFunc("/v1/bash/end", c.handleEnd)
	return mux
}

func (c *AuditCollector) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var event protocol.BashStartEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, fmt.Sprintf("decode start event: %v", err), http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	c.pending[event.ExecID] = event
	c.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (c *AuditCollector) handleEnd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var event protocol.BashEndEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, fmt.Sprintf("decode end event: %v", err), http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	startEvent, ok := c.pending[event.ExecID]
	if ok {
		delete(c.pending, event.ExecID)
	}
	c.mu.Unlock()
	if !ok {
		http.Error(w, "missing start event", http.StatusConflict)
		return
	}

	record := protocol.BashRecord{
		ExecID:             startEvent.ExecID,
		RuntimeInstanceID:  startEvent.RuntimeInstanceID,
		SessionID:          startEvent.SessionID,
		ContainerID:        startEvent.ContainerID,
		WorkspaceID:        startEvent.WorkspaceID,
		WorkspacePathLabel: startEvent.WorkspacePathLabel,
		Command:            startEvent.Command,
		CWD:                startEvent.CWD,
		StartedAt:          startEvent.StartedAt,
		EndedAt:            event.EndedAt,
		ExitCode:           event.ExitCode,
		StdoutPreview:      event.StdoutPreview,
		StderrPreview:      event.StderrPreview,
		StdoutBytes:        event.StdoutBytes,
		StderrBytes:        event.StderrBytes,
		StdoutSHA256:       event.StdoutSHA256,
		StderrSHA256:       event.StderrSHA256,
		StdoutTruncated:    event.StdoutTruncated,
		StderrTruncated:    event.StderrTruncated,
	}
	if err := c.writer.Append(record); err != nil {
		http.Error(w, fmt.Sprintf("persist audit record: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
