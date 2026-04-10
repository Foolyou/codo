package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPathPrecedence(t *testing.T) {
	tempDir := t.TempDir()
	explicitPath := filepath.Join(tempDir, "explicit.json")
	envPath := filepath.Join(tempDir, "env.json")
	homeDir := filepath.Join(tempDir, "home")

	t.Setenv(EnvCodoConfig, envPath)
	t.Setenv(EnvCodoHome, homeDir)

	resolved, err := ResolveConfigPath(explicitPath)
	if err != nil {
		t.Fatalf("ResolveConfigPath explicit: %v", err)
	}
	if resolved.Source != ConfigPathFromFlag {
		t.Fatalf("expected explicit source, got %q", resolved.Source)
	}
	if resolved.Path != explicitPath {
		t.Fatalf("expected explicit path %q, got %q", explicitPath, resolved.Path)
	}

	resolved, err = ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath env: %v", err)
	}
	if resolved.Source != ConfigPathFromEnv {
		t.Fatalf("expected env source, got %q", resolved.Source)
	}
	if resolved.Path != envPath {
		t.Fatalf("expected env path %q, got %q", envPath, resolved.Path)
	}

	t.Setenv(EnvCodoConfig, "")
	resolved, err = ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath default: %v", err)
	}
	if resolved.Source != ConfigPathFromDefault {
		t.Fatalf("expected default source, got %q", resolved.Source)
	}
	wantDefault := filepath.Join(homeDir, DefaultConfigDirName, DefaultConfigFileName)
	if resolved.Path != wantDefault {
		t.Fatalf("expected default path %q, got %q", wantDefault, resolved.Path)
	}
	if resolved.HomeDir != homeDir {
		t.Fatalf("expected home dir %q, got %q", homeDir, resolved.HomeDir)
	}
}

func TestEnsureDefaultHomeConfigCreatesStarterConfig(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "codo-home")
	t.Setenv(EnvCodoHome, homeDir)

	resolved, err := ResolveConfigPath("")
	if err != nil {
		t.Fatalf("ResolveConfigPath: %v", err)
	}

	created, err := EnsureDefaultHomeConfig(resolved)
	if err != nil {
		t.Fatalf("EnsureDefaultHomeConfig: %v", err)
	}
	if !created {
		t.Fatal("expected config creation on first run")
	}

	if info, err := os.Stat(filepath.Join(homeDir, DefaultWorkspaceDir)); err != nil {
		t.Fatalf("stat workspace dir: %v", err)
	} else if !info.IsDir() {
		t.Fatal("workspace path is not a directory")
	}

	cfg, err := Load(resolved.Path)
	if err != nil {
		t.Fatalf("Load created config: %v", err)
	}
	if got, want := cfg.Runtime.WorkspacePath, filepath.Join(homeDir, DefaultWorkspaceDir); got != want {
		t.Fatalf("unexpected workspace path: got %q want %q", got, want)
	}
	if got, want := cfg.Runtime.HostStateDir, filepath.Join(homeDir, DefaultConfigDirName, DefaultStateDir); got != want {
		t.Fatalf("unexpected host state dir: got %q want %q", got, want)
	}

	created, err = EnsureDefaultHomeConfig(resolved)
	if err != nil {
		t.Fatalf("EnsureDefaultHomeConfig second run: %v", err)
	}
	if created {
		t.Fatal("expected existing config to be preserved")
	}
}
