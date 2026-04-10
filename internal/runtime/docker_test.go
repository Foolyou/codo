package runtime

import (
	"path/filepath"
	"testing"

	"github.com/chenan/codo/internal/config"
)

func TestBuildContainerSpecUsesExplicitWorkspaceAndHostSocketsOnly(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Runtime: config.RuntimeConfig{
			Name:                "codo-assistant",
			Image:               "codo:latest",
			WorkspacePath:       "/work/project",
			WorkspaceLabel:      "project",
			WorkspaceMountPath:  "/workspace",
			HostStateDir:        "/state",
			HostControlDir:      "/state/run",
			ContainerControlDir: "/run/codo",
		},
		Provider: config.ProviderConfig{
			Type:      config.DefaultProviderType,
			BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKeyEnv: "BAILIAN_API_KEY",
		},
		Proxy: config.ProxyConfig{
			SocketPath: filepath.Join("/state/run", "model-proxy.sock"),
		},
		Audit: config.AuditConfig{
			SocketPath:   filepath.Join("/state/run", "audit.sock"),
			PreviewBytes: 2048,
		},
	}
	state := State{
		RuntimeInstanceID: "rtm_test",
		ContainerName:     "codo-assistant",
	}

	spec := BuildContainerSpec(cfg, state)

	if len(spec.Mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(spec.Mounts))
	}
	if spec.Mounts[0].Source != "/work/project" || spec.Mounts[0].Target != "/workspace" {
		t.Fatalf("unexpected workspace mount: %+v", spec.Mounts[0])
	}
	if spec.Mounts[1].Source != "/state/run" || spec.Mounts[1].Target != "/run/codo" {
		t.Fatalf("unexpected control mount: %+v", spec.Mounts[1])
	}
	if got := spec.Env[EnvAuditSocket]; got != "/run/codo/audit.sock" {
		t.Fatalf("unexpected audit socket env: %s", got)
	}
	if got := spec.Env[EnvModelProxySocket]; got != "/run/codo/model-proxy.sock" {
		t.Fatalf("unexpected model proxy socket env: %s", got)
	}
	if got := spec.Env[EnvAuditPreviewBytes]; got != "2048" {
		t.Fatalf("unexpected preview bytes env: %s", got)
	}
	if _, ok := spec.Env["BAILIAN_API_KEY"]; ok {
		t.Fatal("container env should not include upstream api key")
	}
	for _, value := range spec.Env {
		if value == cfg.Provider.BaseURL {
			t.Fatal("container env should not include upstream base url")
		}
	}
}

func TestLoadOrCreateStatePersistsRuntimeIdentity(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "runtime-instance.json")
	first, created, err := LoadOrCreateState(statePath, "codo-assistant")
	if err != nil {
		t.Fatalf("LoadOrCreateState first call: %v", err)
	}
	if !created {
		t.Fatal("expected first state call to create state")
	}

	second, created, err := LoadOrCreateState(statePath, "codo-assistant")
	if err != nil {
		t.Fatalf("LoadOrCreateState second call: %v", err)
	}
	if created {
		t.Fatal("expected second state call to reuse state")
	}
	if first.RuntimeInstanceID != second.RuntimeInstanceID {
		t.Fatalf("runtime_instance_id changed across reload: %q != %q", first.RuntimeInstanceID, second.RuntimeInstanceID)
	}
}
