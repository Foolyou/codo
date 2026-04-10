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

type previewWriter struct {
	output  *os.File
	limit   int
	preview bytes.Buffer
	hasher  hash.Hash
	bytes   int64
}

func newPreviewWriter(output *os.File, limit int) *previewWriter {
	return &previewWriter{
		output: output,
		limit:  limit,
		hasher: sha256.New(),
	}
}

func (w *previewWriter) Write(p []byte) (int, error) {
	if _, err := w.output.Write(p); err != nil {
		return 0, err
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

func (w *previewWriter) Summary() (string, int64, string, bool) {
	sum := w.hasher.Sum(nil)
	return w.preview.String(), w.bytes, hex.EncodeToString(sum), w.bytes > int64(w.limit)
}

func RunAuditedBash(ctx context.Context, command string) error {
	auditSocket := os.Getenv(EnvAuditSocket)
	if auditSocket == "" {
		return fmt.Errorf("%s is required", EnvAuditSocket)
	}

	sessionID := os.Getenv(EnvSessionID)
	if sessionID == "" {
		sessionID = ids.NewSessionID()
	}
	runtimeInstanceID := os.Getenv(EnvRuntimeInstanceID)
	if runtimeInstanceID == "" {
		return fmt.Errorf("%s is required", EnvRuntimeInstanceID)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	containerID, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get container hostname: %w", err)
	}

	start := protocol.BashStartEvent{
		ExecID:             ids.NewExecID(),
		RuntimeInstanceID:  runtimeInstanceID,
		SessionID:          sessionID,
		ContainerID:        containerID,
		WorkspaceID:        os.Getenv(EnvWorkspaceID),
		WorkspacePathLabel: os.Getenv(EnvWorkspacePathLabel),
		Command:            command,
		CWD:                cwd,
		StartedAt:          time.Now().UTC(),
	}
	if err := postJSON(ctx, auditSocket, "/v1/bash/start", start); err != nil {
		return fmt.Errorf("audit collector rejected start event: %w", err)
	}

	previewLimit := config.DefaultPreviewBytes
	if rawPreviewLimit := os.Getenv(EnvAuditPreviewBytes); rawPreviewLimit != "" {
		if parsed, err := strconv.Atoi(rawPreviewLimit); err == nil && parsed > 0 {
			previewLimit = parsed
		}
	}
	stdout := newPreviewWriter(os.Stdout, previewLimit)
	stderr := newPreviewWriter(os.Stderr, previewLimit)

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if ok := errorAs(runErr, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	stdoutPreview, stdoutBytes, stdoutSHA, stdoutTruncated := stdout.Summary()
	stderrPreview, stderrBytes, stderrSHA, stderrTruncated := stderr.Summary()
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
		return fmt.Errorf("audit collector rejected completion event: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func ProxyRequest(ctx context.Context, method string, path string, body []byte) error {
	socketPath := os.Getenv(EnvModelProxySocket)
	if socketPath == "" {
		return fmt.Errorf("%s is required", EnvModelProxySocket)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+normalizeProxyPath(path), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create proxy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codo-Runtime-Instance-ID", os.Getenv(EnvRuntimeInstanceID))
	req.Header.Set("X-Codo-Session-ID", sessionID())
	req.Header.Set("X-Codo-Workspace-ID", os.Getenv(EnvWorkspaceID))

	resp, err := transport.NewUnixHTTPClient(socketPath).Do(req)
	if err != nil {
		return fmt.Errorf("send proxy request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read proxy response: %w", err)
	}
	if _, err := os.Stdout.Write(bodyBytes); err != nil {
		return fmt.Errorf("write proxy response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("proxy request failed with status %d", resp.StatusCode)
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
