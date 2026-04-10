package runtime

import (
	"bytes"
	"context"
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
