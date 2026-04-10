package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenan/codo/internal/config"
	codoruntime "github.com/chenan/codo/internal/runtime"
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

func TestRunAssistantChatReusesHealthyControlPlane(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "runtime.json")
	workspacePath := filepath.Join(tempDir, "workspace")
	writeConfigFile(t, configPath, workspacePath)

	var callOrder []string
	app := cli{
		loadConfig: loadConfig,
		controlPlaneHealthy: func(_ context.Context, cfg config.Config) error {
			callOrder = append(callOrder, "health")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			return nil
		},
		serveControlPlaneReady: func(context.Context, config.Config, chan<- struct{}) error {
			t.Fatal("serveControlPlaneReady should not be called when sockets are healthy")
			return nil
		},
		ensureRuntimeImage: func(_ context.Context, cfg config.Config) error {
			callOrder = append(callOrder, "image")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			return nil
		},
		startRuntime: func(_ context.Context, cfg config.Config) error {
			callOrder = append(callOrder, "runtime")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			return nil
		},
		probeAssistantREPL: func(_ context.Context, cfg config.Config, opts codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "probe")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			if opts.SessionID != "sess_test" {
				t.Fatalf("unexpected session id: %q", opts.SessionID)
			}
			return nil
		},
		attachAssistantREPL: func(_ context.Context, cfg config.Config, opts codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "attach")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			if opts.SessionID != "sess_test" {
				t.Fatalf("unexpected session id: %q", opts.SessionID)
			}
			if opts.Model != "qwen-test" {
				t.Fatalf("unexpected model: %q", opts.Model)
			}
			if opts.MaxToolCalls != 3 {
				t.Fatalf("unexpected max tool calls: %d", opts.MaxToolCalls)
			}
			if opts.BashTimeout != 2*time.Second {
				t.Fatalf("unexpected bash timeout: %s", opts.BashTimeout)
			}
			if opts.BashOutputBytes != 2048 {
				t.Fatalf("unexpected bash output bytes: %d", opts.BashOutputBytes)
			}
			return nil
		},
	}

	err := app.run(context.Background(), []string{
		"assistant", "chat",
		"--config", configPath,
		"--session-id", "sess_test",
		"--model", "qwen-test",
		"--max-tool-calls", "3",
		"--bash-timeout", "2s",
		"--bash-output-bytes", "2048",
	})
	if err != nil {
		t.Fatalf("run assistant chat: %v", err)
	}

	wantOrder := []string{"health", "image", "runtime", "probe", "attach"}
	if fmt.Sprint(callOrder) != fmt.Sprint(wantOrder) {
		t.Fatalf("unexpected call order: got %v want %v", callOrder, wantOrder)
	}
}

func TestRunAssistantChatStartsSessionScopedControlPlaneWhenUnhealthy(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "runtime.json")
	workspacePath := filepath.Join(tempDir, "workspace")
	writeConfigFile(t, configPath, workspacePath)

	controlPlaneStopped := make(chan struct{})
	var callOrder []string
	app := cli{
		loadConfig: loadConfig,
		controlPlaneHealthy: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "health")
			return errors.New("sockets unavailable")
		},
		serveControlPlaneReady: func(ctx context.Context, cfg config.Config, ready chan<- struct{}) error {
			callOrder = append(callOrder, "serve")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			close(ready)
			<-ctx.Done()
			close(controlPlaneStopped)
			return nil
		},
		ensureRuntimeImage: func(_ context.Context, cfg config.Config) error {
			callOrder = append(callOrder, "image")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			return nil
		},
		startRuntime: func(_ context.Context, cfg config.Config) error {
			callOrder = append(callOrder, "runtime")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			return nil
		},
		probeAssistantREPL: func(context.Context, config.Config, codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "probe")
			return nil
		},
		attachAssistantREPL: func(_ context.Context, _ config.Config, _ codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "attach")
			return nil
		},
	}

	if err := app.run(context.Background(), []string{"assistant", "chat", "--config", configPath}); err != nil {
		t.Fatalf("run assistant chat: %v", err)
	}

	select {
	case <-controlPlaneStopped:
	default:
		t.Fatal("expected session-scoped control plane to stop after attach")
	}

	wantOrder := []string{"health", "serve", "image", "runtime", "probe", "attach"}
	if fmt.Sprint(callOrder) != fmt.Sprint(wantOrder) {
		t.Fatalf("unexpected call order: got %v want %v", callOrder, wantOrder)
	}
}

func TestRunAssistantChatRepairsOutdatedRuntimeBeforeAttach(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "runtime.json")
	workspacePath := filepath.Join(tempDir, "workspace")
	writeConfigFile(t, configPath, workspacePath)

	var callOrder []string
	probeCalls := 0
	app := cli{
		loadConfig: loadConfig,
		controlPlaneHealthy: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "health")
			return nil
		},
		ensureRuntimeImage: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "image")
			return nil
		},
		startRuntime: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "runtime")
			return nil
		},
		probeAssistantREPL: func(_ context.Context, cfg config.Config, _ codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "probe")
			if cfg.Runtime.WorkspacePath != workspacePath {
				t.Fatalf("unexpected workspace path: got %q want %q", cfg.Runtime.WorkspacePath, workspacePath)
			}
			probeCalls++
			if probeCalls == 1 {
				return fmt.Errorf("%w: usage: codo <control-plane|runtime> ...", codoruntime.ErrAssistantRuntimeOutOfDate)
			}
			return nil
		},
		buildRuntimeImage: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "build")
			return nil
		},
		rebuildRuntime: func(context.Context, config.Config) error {
			callOrder = append(callOrder, "rebuild")
			return nil
		},
		attachAssistantREPL: func(context.Context, config.Config, codoruntime.AssistantREPLOptions) error {
			callOrder = append(callOrder, "attach")
			return nil
		},
	}

	if err := app.run(context.Background(), []string{"assistant", "chat", "--config", configPath}); err != nil {
		t.Fatalf("run assistant chat: %v", err)
	}

	wantOrder := []string{"health", "image", "runtime", "probe", "build", "rebuild", "probe", "attach"}
	if fmt.Sprint(callOrder) != fmt.Sprint(wantOrder) {
		t.Fatalf("unexpected call order: got %v want %v", callOrder, wantOrder)
	}
}

func TestRunAssistantReplParsesFlags(t *testing.T) {
	app := cli{
		runAssistantREPL: func(_ context.Context, opts codoruntime.AssistantREPLOptions) error {
			if opts.SessionID != "sess_test" {
				t.Fatalf("unexpected session id: %q", opts.SessionID)
			}
			if opts.Model != "qwen-test" {
				t.Fatalf("unexpected model: %q", opts.Model)
			}
			if opts.WorkspaceRoot != "/workspace" {
				t.Fatalf("unexpected workspace root: %q", opts.WorkspaceRoot)
			}
			if opts.MaxToolCalls != 5 {
				t.Fatalf("unexpected max tool calls: %d", opts.MaxToolCalls)
			}
			if opts.BashTimeout != 45*time.Second {
				t.Fatalf("unexpected bash timeout: %s", opts.BashTimeout)
			}
			if opts.BashOutputBytes != 1234 {
				t.Fatalf("unexpected bash output bytes: %d", opts.BashOutputBytes)
			}
			return nil
		},
	}

	if err := app.run(context.Background(), []string{
		"assistant", "repl",
		"--session-id", "sess_test",
		"--model", "qwen-test",
		"--workspace-root", "/workspace",
		"--max-tool-calls", "5",
		"--bash-timeout", "45s",
		"--bash-output-bytes", "1234",
	}); err != nil {
		t.Fatalf("run assistant repl: %v", err)
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
