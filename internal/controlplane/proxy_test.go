package controlplane

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/jsonl"
	"github.com/chenan/codo/internal/protocol"
	"github.com/chenan/codo/internal/provider"
)

func TestModelProxyInjectsCredentialsAndPersistsAuditMetadata(t *testing.T) {
	var receivedAuth string
	var receivedPath string
	t.Setenv("TEST_BAILIAN_API_KEY", "secret-token")

	adapter, err := provider.New(config.ProviderConfig{
		Type:      config.DefaultProviderType,
		BaseURL:   "https://bailian.invalid/compatible-mode/v1",
		APIKeyEnv: "TEST_BAILIAN_API_KEY",
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "model-proxy.jsonl")

	proxy := NewModelProxy(adapter, jsonl.NewWriter(logPath))
	proxy.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedAuth = req.Header.Get("Authorization")
			receivedPath = req.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

	body := []byte(`{"model":"qwen-max","messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req_1")
	req.Header.Set("X-Codo-Runtime-Instance-ID", "rtm_1")
	req.Header.Set("X-Codo-Session-ID", "sess_1")
	req.Header.Set("X-Codo-Workspace-ID", "workspace_1")

	recorder := httptest.NewRecorder()
	proxy.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"ok":true}` {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}

	if receivedAuth != "Bearer secret-token" {
		t.Fatalf("expected Authorization header to be injected, got %q", receivedAuth)
	}
	if receivedPath != "/compatible-mode/v1/chat/completions" {
		t.Fatalf("unexpected forwarded path %q", receivedPath)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	var record protocol.ProxyAuditRecord
	if err := json.Unmarshal(bytes.TrimSpace(logBytes), &record); err != nil {
		t.Fatalf("Unmarshal log record: %v", err)
	}
	if record.RequestID != "req_1" || record.RuntimeInstanceID != "rtm_1" || record.SessionID != "sess_1" {
		t.Fatalf("unexpected correlation metadata: %+v", record)
	}
	if record.TargetModel != "qwen-max" || record.ResponseStatus != http.StatusOK {
		t.Fatalf("unexpected proxy audit metadata: %+v", record)
	}
}

func TestModelProxyStreamsResponsesAndFlushes(t *testing.T) {
	t.Setenv("TEST_BAILIAN_API_KEY", "secret-token")

	adapter, err := provider.New(config.ProviderConfig{
		Type:      config.DefaultProviderType,
		BaseURL:   "https://bailian.invalid/compatible-mode/v1",
		APIKeyEnv: "TEST_BAILIAN_API_KEY",
	})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "model-proxy.jsonl")

	proxy := NewModelProxy(adapter, jsonl.NewWriter(logPath))
	proxy.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
						"data: [DONE]\n\n",
				)),
			}, nil
		}),
	}

	body := []byte(`{"model":"qwen-max","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codo-Session-ID", "sess_stream")

	recorder := httptest.NewRecorder()
	proxy.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code %d", recorder.Code)
	}
	if !recorder.Flushed {
		t.Fatal("expected streamed response to flush")
	}

	wantBody := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	if recorder.Body.String() != wantBody {
		t.Fatalf("unexpected streamed response body: %q", recorder.Body.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	var record protocol.ProxyAuditRecord
	if err := json.Unmarshal(bytes.TrimSpace(logBytes), &record); err != nil {
		t.Fatalf("Unmarshal log record: %v", err)
	}
	if record.SessionID != "sess_stream" || record.ResponseStatus != http.StatusOK {
		t.Fatalf("unexpected proxy audit record: %+v", record)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
