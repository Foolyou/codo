package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/ids"
	"github.com/chenan/codo/internal/protocol"
	"github.com/chenan/codo/internal/transport"
)

type BashExecutionRequest struct {
	Command      string
	Workdir      string
	Stdout       io.Writer
	Stderr       io.Writer
	CaptureLimit int
}

type BashExecutionResult struct {
	ExecID             string `json:"exec_id"`
	RuntimeInstanceID  string `json:"runtime_instance_id"`
	SessionID          string `json:"session_id"`
	WorkspaceID        string `json:"workspace_id,omitempty"`
	WorkspacePathLabel string `json:"workspace_path_label,omitempty"`
	Command            string `json:"command"`
	CWD                string `json:"cwd"`
	ExitCode           int    `json:"exit_code"`
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	StdoutBytes        int64  `json:"stdout_bytes"`
	StderrBytes        int64  `json:"stderr_bytes"`
	StdoutSHA256       string `json:"stdout_sha256"`
	StderrSHA256       string `json:"stderr_sha256"`
	StdoutTruncated    bool   `json:"stdout_truncated"`
	StderrTruncated    bool   `json:"stderr_truncated"`
	TimedOut           bool   `json:"timed_out"`
	RunError           error  `json:"-"`
}

type captureWriter struct {
	output  io.Writer
	limit   int
	preview bytes.Buffer
	hasher  hash.Hash
	bytes   int64
}

func newCaptureWriter(output io.Writer, limit int) *captureWriter {
	return &captureWriter{
		output: output,
		limit:  limit,
		hasher: sha256.New(),
	}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.output != nil {
		if _, err := w.output.Write(p); err != nil {
			return 0, err
		}
	}
	originalLength := len(p)
	w.bytes += int64(len(p))
	if _, err := w.hasher.Write(p); err != nil {
		return 0, err
	}

	remaining := w.limit - w.preview.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		if _, err := w.preview.Write(p); err != nil {
			return 0, err
		}
	}
	return originalLength, nil
}

func (w *captureWriter) Summary() (string, int64, string, bool) {
	sum := w.hasher.Sum(nil)
	return w.preview.String(), w.bytes, hex.EncodeToString(sum), w.bytes > int64(w.limit)
}

func ExecuteAuditedBash(ctx context.Context, req BashExecutionRequest) (BashExecutionResult, error) {
	auditSocket := os.Getenv(EnvAuditSocket)
	if auditSocket == "" {
		return BashExecutionResult{}, fmt.Errorf("%s is required", EnvAuditSocket)
	}

	runtimeInstanceID := os.Getenv(EnvRuntimeInstanceID)
	if runtimeInstanceID == "" {
		return BashExecutionResult{}, fmt.Errorf("%s is required", EnvRuntimeInstanceID)
	}

	cwd := req.Workdir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return BashExecutionResult{}, fmt.Errorf("get cwd: %w", err)
		}
	}

	containerID, err := os.Hostname()
	if err != nil {
		return BashExecutionResult{}, fmt.Errorf("get container hostname: %w", err)
	}

	start := protocol.BashStartEvent{
		ExecID:             ids.NewExecID(),
		RuntimeInstanceID:  runtimeInstanceID,
		SessionID:          sessionID(),
		ContainerID:        containerID,
		WorkspaceID:        os.Getenv(EnvWorkspaceID),
		WorkspacePathLabel: os.Getenv(EnvWorkspacePathLabel),
		Command:            req.Command,
		CWD:                cwd,
		StartedAt:          time.Now().UTC(),
	}
	if err := postJSON(ctx, auditSocket, "/v1/bash/start", start); err != nil {
		return BashExecutionResult{}, fmt.Errorf("audit collector rejected start event: %w", err)
	}

	limit := req.CaptureLimit
	if limit <= 0 {
		limit = bashCaptureLimit()
	}

	stdout := newCaptureWriter(req.Stdout, limit)
	stderr := newCaptureWriter(req.Stderr, limit)

	cmd := exec.CommandContext(ctx, "bash", "-lc", req.Command)
	cmd.Dir = cwd
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()

	exitCode := exitCodeForRunError(runErr)
	timedOut := errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	if timedOut && exitCode == 0 {
		exitCode = 124
	}

	stdoutPreview, stdoutBytes, stdoutSHA, stdoutTruncated := stdout.Summary()
	stderrPreview, stderrBytes, stderrSHA, stderrTruncated := stderr.Summary()
	result := BashExecutionResult{
		ExecID:             start.ExecID,
		RuntimeInstanceID:  start.RuntimeInstanceID,
		SessionID:          start.SessionID,
		WorkspaceID:        start.WorkspaceID,
		WorkspacePathLabel: start.WorkspacePathLabel,
		Command:            start.Command,
		CWD:                start.CWD,
		ExitCode:           exitCode,
		Stdout:             stdoutPreview,
		Stderr:             stderrPreview,
		StdoutBytes:        stdoutBytes,
		StderrBytes:        stderrBytes,
		StdoutSHA256:       stdoutSHA,
		StderrSHA256:       stderrSHA,
		StdoutTruncated:    stdoutTruncated,
		StderrTruncated:    stderrTruncated,
		TimedOut:           timedOut,
		RunError:           runErr,
	}

	end := protocol.BashEndEvent{
		ExecID:          start.ExecID,
		EndedAt:         time.Now().UTC(),
		ExitCode:        exitCode,
		StdoutPreview:   stdoutPreview,
		StderrPreview:   stderrPreview,
		StdoutBytes:     stdoutBytes,
		StderrBytes:     stderrBytes,
		StdoutSHA256:    stdoutSHA,
		StderrSHA256:    stderrSHA,
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
	}
	if err := postJSON(ctx, auditSocket, "/v1/bash/end", end); err != nil {
		return result, fmt.Errorf("audit collector rejected completion event: %w", err)
	}

	return result, nil
}

func RunAuditedBash(ctx context.Context, command string) error {
	result, err := ExecuteAuditedBash(ctx, BashExecutionRequest{
		Command: command,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	if err != nil {
		return err
	}
	if result.RunError != nil {
		return result.RunError
	}
	return nil
}

func ProxyRoundTrip(ctx context.Context, method string, path string, body []byte) ([]byte, int, error) {
	socketPath := os.Getenv(EnvModelProxySocket)
	if socketPath == "" {
		return nil, 0, fmt.Errorf("%s is required", EnvModelProxySocket)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+normalizeProxyPath(path), bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create proxy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codo-Runtime-Instance-ID", os.Getenv(EnvRuntimeInstanceID))
	req.Header.Set("X-Codo-Session-ID", sessionID())
	req.Header.Set("X-Codo-Workspace-ID", os.Getenv(EnvWorkspaceID))

	resp, err := transport.NewUnixHTTPClient(socketPath).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send proxy request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read proxy response: %w", err)
	}
	return bodyBytes, resp.StatusCode, nil
}

func ProxyRequest(ctx context.Context, method string, path string, body []byte) error {
	bodyBytes, statusCode, err := ProxyRoundTrip(ctx, method, path, body)
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(bodyBytes); err != nil {
		return fmt.Errorf("write proxy response: %w", err)
	}
	if statusCode >= 400 {
		return fmt.Errorf("proxy request failed with status %d", statusCode)
	}
	return nil
}

func postJSON(ctx context.Context, socketPath string, path string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := transport.NewUnixHTTPClient(socketPath).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func normalizeProxyPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func sessionID() string {
	if current := os.Getenv(EnvSessionID); current != "" {
		return current
	}
	return ids.NewSessionID()
}

func errorAs(err error, target any) bool {
	return errors.As(err, target)
}

func bashCaptureLimit() int {
	previewLimit := config.DefaultPreviewBytes
	if rawPreviewLimit := os.Getenv(EnvAuditPreviewBytes); rawPreviewLimit != "" {
		if parsed, err := strconv.Atoi(rawPreviewLimit); err == nil && parsed > 0 {
			previewLimit = parsed
		}
	}
	return previewLimit
}

func exitCodeForRunError(runErr error) int {
	if runErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := errorAs(runErr, &exitErr); ok {
		return exitErr.ExitCode()
	}
	if errors.Is(runErr, context.DeadlineExceeded) {
		return 124
	}
	return 1
}
