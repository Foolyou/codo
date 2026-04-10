package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chenan/codo/internal/config"
)

func TestRunControlPlaneServeUsesDefaultHomeConfig(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "codo-home")
	t.Setenv(config.EnvCodoHome, homeDir)

	var served bool
	wantWorkspace := filepath.Join(homeDir, config.DefaultWorkspaceDir)
	resolved, err := config.ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath: %v", err)
	}
	if _, err := config.EnsureDefaultHomeConfig(resolved); err != nil {
		t.Fatalf("EnsureDefaultHomeConfig: %v", err)
	}

	app := cli{
		loadConfig: loadConfig,
		serveControlPlane: func(_ context.Context, cfg config.Config) error {
			if cfg.Runtime.WorkspacePath != wantWorkspace {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, wantWorkspace)
			}
			served = true
			return nil
		},
	}

	if err := app.run(context.Background(), []string{"control-plane", "serve"}); err != nil {
		t.Fatalf("run control-plane serve: %v", err)
	}

	if !served {
		t.Fatal("expected serveControlPlane to be called")
	}
}

func TestRunRuntimeCommandsPreserveExplicitAndEnvConfigResolution(t *testing.T) {
	tempDir := t.TempDir()
	explicitPath := filepath.Join(tempDir, "explicit.json")
	envPath := filepath.Join(tempDir, "env.json")
	t.Setenv(config.EnvCodoConfig, envPath)

	explicitWorkspace := filepath.Join(tempDir, "explicit-workspace")
	envWorkspace := filepath.Join(tempDir, "env-workspace")
	writeConfigFile(t, explicitPath, explicitWorkspace)
	writeConfigFile(t, envPath, envWorkspace)

	t.Run("build-image uses explicit config", func(t *testing.T) {
		var built bool
		app := cli{
			loadConfig: loadConfig,
			buildRuntimeImage: func(_ context.Context, cfg config.Config) error {
				if cfg.Runtime.WorkspacePath != explicitWorkspace {
					t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, explicitWorkspace)
				}
				built = true
				return nil
			},
		}

		if err := app.run(context.Background(), []string{"runtime", "build-image", "--config", explicitPath}); err != nil {
			t.Fatalf("run runtime build-image: %v", err)
		}
		if !built {
			t.Fatal("expected buildRuntimeImage to be called")
		}
	})

	t.Run("reconnect uses CODO_CONFIG when flag is omitted", func(t *testing.T) {
		var reconnected bool
		app := cli{
			loadConfig: loadConfig,
			reconnectRuntime: func(_ context.Context, cfg config.Config, sessionID string) error {
				if sessionID != "" {
					t.Fatalf("expected empty session override, got %q", sessionID)
				}
				if cfg.Runtime.WorkspacePath != envWorkspace {
					t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, envWorkspace)
				}
				reconnected = true
				return nil
			},
		}

		if err := app.run(context.Background(), []string{"runtime", "reconnect"}); err != nil {
			t.Fatalf("run runtime reconnect: %v", err)
		}
		if !reconnected {
			t.Fatal("expected reconnectRuntime to be called")
		}
	})
}

func TestRunUpPassesResolvedFlagValueToBootstrap(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "custom.json")

	var gotConfigPath string
	app := cli{
		up: func(_ context.Context, got string) error {
			gotConfigPath = got
			return nil
		},
	}

	if err := app.run(context.Background(), []string{"up", "--config", configPath}); err != nil {
		t.Fatalf("run up: %v", err)
	}
	if gotConfigPath != configPath {
		t.Fatalf("unexpected up config path: got %q want %q", gotConfigPath, configPath)
	}
}

func writeConfigFile(t *testing.T, path string, workspacePath string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace dir: %v", err)
	}

	content := fmt.Sprintf(`{
  "runtime": {
    "name": "codo-assistant",
    "image": "codo:latest",
    "workspace_path": %q,
    "workspace_mount_path": "/workspace",
    "host_state_dir": %q,
    "container_control_dir": "/run/codo"
  },
  "provider": {
    "type": "bailian-openai-compatible",
    "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "api_key_env": "BAILIAN_API_KEY"
  },
  "audit": {
    "preview_bytes": 4096
  }
}
`, workspacePath, filepath.Join(filepath.Dir(path), "state"))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
}
