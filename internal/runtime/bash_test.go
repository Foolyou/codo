package runtime

import (
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
