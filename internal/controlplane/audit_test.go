package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chenan/codo/internal/protocol"
)

func TestAuditCollectorPersistsCompletedBashRecord(t *testing.T) {
	t.Parallel()

	logPath := t.TempDir() + "/bash-audit.jsonl"
	handler := NewAuditCollector(logPath).Handler()
	start := protocol.BashStartEvent{
		ExecID:             "exec_1",
		RuntimeInstanceID:  "rtm_1",
		SessionID:          "sess_1",
		ContainerID:        "container_1",
		WorkspaceID:        "workspace_1",
		WorkspacePathLabel: "workspace",
		Command:            "echo hi",
		CWD:                "/workspace",
		StartedAt:          time.Unix(1700000000, 0).UTC(),
	}
	end := protocol.BashEndEvent{
		ExecID:          "exec_1",
		EndedAt:         time.Unix(1700000001, 0).UTC(),
		ExitCode:        0,
		StdoutPreview:   "hi\n",
		StderrPreview:   "",
		StdoutBytes:     3,
		StderrBytes:     0,
		StdoutSHA256:    "abc",
		StderrSHA256:    "def",
		StdoutTruncated: false,
		StderrTruncated: false,
	}

	serveJSON(t, handler, "/v1/bash/start", start)
	serveJSON(t, handler, "/v1/bash/end", end)

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	var record protocol.BashRecord
	if err := json.Unmarshal(bytes.TrimSpace(logBytes), &record); err != nil {
		t.Fatalf("Unmarshal log record: %v", err)
	}
	if record.ExecID != "exec_1" || record.SessionID != "sess_1" || record.Command != "echo hi" {
		t.Fatalf("unexpected audit record: %+v", record)
	}
	if record.StdoutPreview != "hi\n" || record.StdoutBytes != 3 {
		t.Fatalf("unexpected stdout audit fields: %+v", record)
	}
}

func serveJSON(t *testing.T, handler http.Handler, path string, payload any) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://unix"+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code >= 300 {
		t.Fatalf("unexpected response status %d", recorder.Code)
	}
}
