package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/chenan/codo/internal/config"
)

func TestRunUpBootstrapsDefaultHomeAndStartsServices(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolved := config.ResolvedConfigPath{
		Path:    filepath.Join(t.TempDir(), "config", "runtime.json"),
		HomeDir: filepath.Join(t.TempDir(), "home"),
		Source:  config.ConfigPathFromDefault,
	}
	cfg := testConfig()

	var mu sync.Mutex
	var calls []string
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, name)
	}

	err := runUp(ctx, "", dependencies{
		resolveConfigPath: func(explicit string) (config.ResolvedConfigPath, error) {
			record("resolve")
			return resolved, nil
		},
		ensureDefaultConfig: func(got config.ResolvedConfigPath) (bool, error) {
			record("init")
			if got != resolved {
				t.Fatalf("unexpected resolved path: %+v", got)
			}
			return true, nil
		},
		loadConfig: func(path string) (config.Config, error) {
			record("load")
			if path != resolved.Path {
				t.Fatalf("unexpected load path %q", path)
			}
			return cfg, nil
		},
		stat: func(path string) (os.FileInfo, error) {
			t.Fatalf("stat should not be called for default config path %q", path)
			return nil, nil
		},
		ensureImage: func(ctx context.Context, got config.Config) error {
			record("image")
			if got.Runtime.Name != cfg.Runtime.Name {
				t.Fatalf("unexpected config passed to ensureImage: %+v", got)
			}
			return nil
		},
		serveControlPlane: func(ctx context.Context, got config.Config, ready chan<- struct{}) error {
			record("serve")
			if got.Runtime.Name != cfg.Runtime.Name {
				t.Fatalf("unexpected config passed to serveControlPlane: %+v", got)
			}
			close(ready)
			<-ctx.Done()
			return nil
		},
		ensureRuntime: func(ctx context.Context, got config.Config) error {
			record("runtime")
			if got.Runtime.Name != cfg.Runtime.Name {
				t.Fatalf("unexpected config passed to ensureRuntime: %+v", got)
			}
			cancel()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runUp: %v", err)
	}

	wantCalls := []string{"resolve", "init", "load", "image", "serve", "runtime"}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("unexpected call order: got %v want %v", calls, wantCalls)
	}
}

func TestRunUpRejectsMissingCustomConfig(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "missing.json")
	resolved := config.ResolvedConfigPath{
		Path:   missingPath,
		Source: config.ConfigPathFromFlag,
	}

	err := runUp(context.Background(), missingPath, dependencies{
		resolveConfigPath: func(explicit string) (config.ResolvedConfigPath, error) {
			return resolved, nil
		},
		ensureDefaultConfig: func(config.ResolvedConfigPath) (bool, error) {
			t.Fatal("ensureDefaultConfig should not be called for custom paths")
			return false, nil
		},
		loadConfig: func(string) (config.Config, error) {
			t.Fatal("loadConfig should not be called for missing custom paths")
			return config.Config{}, nil
		},
		stat: func(path string) (os.FileInfo, error) {
			if path != missingPath {
				t.Fatalf("unexpected stat path %q", path)
			}
			return nil, os.ErrNotExist
		},
		ensureImage: func(context.Context, config.Config) error {
			t.Fatal("ensureImage should not be called for missing custom paths")
			return nil
		},
		serveControlPlane: func(context.Context, config.Config, chan<- struct{}) error {
			t.Fatal("serveControlPlane should not be called for missing custom paths")
			return nil
		},
		ensureRuntime: func(context.Context, config.Config) error {
			t.Fatal("ensureRuntime should not be called for missing custom paths")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected runUp to fail for missing custom config")
	}
	if got, want := err.Error(), fmt.Sprintf("config file does not exist: %s", missingPath); got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func testConfig() config.Config {
	return config.Config{
		Runtime: config.RuntimeConfig{
			Name:                "codo-assistant",
			Image:               "codo:latest",
			WorkspacePath:       "/tmp/workspace",
			WorkspaceLabel:      "workspace",
			WorkspaceMountPath:  "/workspace",
			HostStateDir:        "/tmp/state",
			HostControlDir:      "/tmp/state/run",
			ContainerControlDir: "/run/codo",
		},
		Provider: config.ProviderConfig{
			Type:      config.DefaultProviderType,
			BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKeyEnv: "BAILIAN_API_KEY",
		},
		Audit: config.AuditConfig{
			SocketPath:   "/tmp/state/run/audit.sock",
			LogPath:      "/tmp/state/logs/bash-audit.jsonl",
			PreviewBytes: config.DefaultPreviewBytes,
		},
		Proxy: config.ProxyConfig{
			SocketPath:   "/tmp/state/run/model-proxy.sock",
			AuditLogPath: "/tmp/state/logs/model-proxy.jsonl",
		},
	}
}
