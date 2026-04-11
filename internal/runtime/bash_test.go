package runtime

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRunAuditedBashFailsClosedWhenAuditCollectorIsUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "should-not-exist")

	t.Setenv(EnvAuditSocket, filepath.Join(tmpDir, "missing.sock"))
	t.Setenv(EnvRuntimeInstanceID, "rtm_test")
	t.Setenv(EnvSessionID, "sess_test")
	t.Setenv(EnvWorkspaceID, "workspace")
	t.Setenv(EnvWorkspacePathLabel, "workspace")

	err := RunAuditedBash(context.Background(), "touch '"+targetFile+"'")
	if err == nil {
		t.Fatal("expected RunAuditedBash to fail when audit collector is unavailable")
	}
	if _, statErr := os.Stat(targetFile); !os.IsNotExist(statErr) {
		t.Fatalf("command should not have executed, stat err=%v", statErr)
	}
}

func TestCaptureWriterSummaryTracksPreviewAndTruncation(t *testing.T) {
	var passthrough bytes.Buffer
	writer := newCaptureWriter(&passthrough, 4)

	if _, err := writer.Write([]byte("123456789")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	preview, totalBytes, sha, truncated := writer.Summary()
	if preview != "1234" {
		t.Fatalf("unexpected preview: %q", preview)
	}
	if totalBytes != 9 {
		t.Fatalf("unexpected byte count: %d", totalBytes)
	}
	if !truncated {
		t.Fatal("expected output to be marked truncated")
	}
	if sha == "" {
		t.Fatal("expected sha256 summary to be populated")
	}
	if passthrough.String() != "123456789" {
		t.Fatalf("unexpected passthrough output: %q", passthrough.String())
	}
}

func TestProxyStreamRequestReturnsLiveStreamingBody(t *testing.T) {
	allowSecondChunk := make(chan struct{})
	var gotPath string
	oldClientFactory := newProxyHTTPClient
	newProxyHTTPClient = func(string) *http.Client {
		return &http.Client{
			Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotPath = req.URL.Path

				reader, writer := io.Pipe()
				go func() {
					_, _ = io.WriteString(writer, "data: first\n\n")
					<-allowSecondChunk
					_, _ = io.WriteString(writer, "data: second\n\n")
					_ = writer.Close()
				}()

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       reader,
				}, nil
			}),
		}
	}
	defer func() {
		newProxyHTTPClient = oldClientFactory
	}()

	t.Setenv(EnvModelProxySocket, filepath.Join(t.TempDir(), "model-proxy.sock"))
	t.Setenv(EnvRuntimeInstanceID, "rtm_test")
	t.Setenv(EnvSessionID, "sess_test")
	t.Setenv(EnvWorkspaceID, "workspace")

	resp, err := ProxyStreamRequest(context.Background(), http.MethodPost, "/v1/chat/completions", []byte(`{"stream":true}`))
	if err != nil {
		t.Fatalf("ProxyStreamRequest: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first streamed line: %v", err)
	}
	if firstLine != "data: first\n" {
		t.Fatalf("unexpected first streamed line: %q", firstLine)
	}

	blankLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read blank separator: %v", err)
	}
	if blankLine != "\n" {
		t.Fatalf("unexpected blank separator: %q", blankLine)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("unexpected streamed request path: %q", gotPath)
	}

	close(allowSecondChunk)

	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read remaining stream: %v", err)
	}
	if string(rest) != "data: second\n\n" {
		t.Fatalf("unexpected remaining stream: %q", string(rest))
	}
}

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn proxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
