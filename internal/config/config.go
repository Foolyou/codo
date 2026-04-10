package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRuntimeName         = "codo-assistant"
	DefaultImage               = "codo:latest"
	DefaultWorkspaceMountPath  = "/workspace"
	DefaultContainerControlDir = "/run/codo"
	DefaultHostStateDir        = ".runtime"
	DefaultProviderType        = "bailian-openai-compatible"
	DefaultPreviewBytes        = 4096
)

type Config struct {
	Runtime  RuntimeConfig  `json:"runtime"`
	Provider ProviderConfig `json:"provider"`
	Proxy    ProxyConfig    `json:"proxy"`
	Audit    AuditConfig    `json:"audit"`
}

type RuntimeConfig struct {
	Name                string `json:"name"`
	Image               string `json:"image"`
	WorkspacePath       string `json:"workspace_path"`
	WorkspaceLabel      string `json:"workspace_label"`
	WorkspaceMountPath  string `json:"workspace_mount_path"`
	HostStateDir        string `json:"host_state_dir"`
	HostControlDir      string `json:"host_control_dir"`
	ContainerControlDir string `json:"container_control_dir"`
}

type ProviderConfig struct {
	Type      string `json:"type"`
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
}

type ProxyConfig struct {
	SocketPath   string `json:"socket_path"`
	AuditLogPath string `json:"audit_log_path"`
}

type AuditConfig struct {
	SocketPath   string `json:"socket_path"`
	LogPath      string `json:"log_path"`
	PreviewBytes int    `json:"preview_bytes"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config json: %w", err)
	}

	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, fmt.Errorf("resolve config base dir: %w", err)
	}
	cfg.applyDefaults(baseDir)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults(baseDir string) {
	if c.Runtime.Name == "" {
		c.Runtime.Name = DefaultRuntimeName
	}
	if c.Runtime.Image == "" {
		c.Runtime.Image = DefaultImage
	}
	if c.Runtime.WorkspaceMountPath == "" {
		c.Runtime.WorkspaceMountPath = DefaultWorkspaceMountPath
	}
	if c.Runtime.ContainerControlDir == "" {
		c.Runtime.ContainerControlDir = DefaultContainerControlDir
	}
	if c.Runtime.HostStateDir == "" {
		c.Runtime.HostStateDir = DefaultHostStateDir
	}
	if c.Provider.Type == "" {
		c.Provider.Type = DefaultProviderType
	}
	if c.Audit.PreviewBytes == 0 {
		c.Audit.PreviewBytes = DefaultPreviewBytes
	}

	c.Runtime.WorkspacePath = resolvePath(baseDir, c.Runtime.WorkspacePath)
	c.Runtime.HostStateDir = resolvePath(baseDir, c.Runtime.HostStateDir)
	if c.Runtime.HostControlDir == "" {
		c.Runtime.HostControlDir = filepath.Join(c.Runtime.HostStateDir, "run")
	} else {
		c.Runtime.HostControlDir = resolvePath(baseDir, c.Runtime.HostControlDir)
	}

	if c.Proxy.SocketPath == "" {
		c.Proxy.SocketPath = filepath.Join(c.Runtime.HostControlDir, "model-proxy.sock")
	} else {
		c.Proxy.SocketPath = resolvePath(baseDir, c.Proxy.SocketPath)
	}
	if c.Audit.SocketPath == "" {
		c.Audit.SocketPath = filepath.Join(c.Runtime.HostControlDir, "audit.sock")
	} else {
		c.Audit.SocketPath = resolvePath(baseDir, c.Audit.SocketPath)
	}
	if c.Proxy.AuditLogPath == "" {
		c.Proxy.AuditLogPath = filepath.Join(c.Runtime.HostStateDir, "logs", "model-proxy.jsonl")
	} else {
		c.Proxy.AuditLogPath = resolvePath(baseDir, c.Proxy.AuditLogPath)
	}
	if c.Audit.LogPath == "" {
		c.Audit.LogPath = filepath.Join(c.Runtime.HostStateDir, "logs", "bash-audit.jsonl")
	} else {
		c.Audit.LogPath = resolvePath(baseDir, c.Audit.LogPath)
	}

	if c.Runtime.WorkspaceLabel == "" && c.Runtime.WorkspacePath != "" {
		c.Runtime.WorkspaceLabel = filepath.Base(c.Runtime.WorkspacePath)
	}
}

func (c Config) validate() error {
	if c.Runtime.WorkspacePath == "" {
		return fmt.Errorf("runtime.workspace_path is required")
	}

	workspaceInfo, err := os.Stat(c.Runtime.WorkspacePath)
	if err != nil {
		return fmt.Errorf("stat workspace_path: %w", err)
	}
	if !workspaceInfo.IsDir() {
		return fmt.Errorf("workspace_path must be a directory")
	}

	if !filepath.IsAbs(c.Runtime.WorkspacePath) {
		return fmt.Errorf("workspace_path must resolve to an absolute path")
	}
	if !strings.HasPrefix(c.Runtime.WorkspaceMountPath, "/") {
		return fmt.Errorf("runtime.workspace_mount_path must be absolute in container")
	}
	if !strings.HasPrefix(c.Runtime.ContainerControlDir, "/") {
		return fmt.Errorf("runtime.container_control_dir must be absolute in container")
	}
	if c.Provider.BaseURL == "" {
		return fmt.Errorf("provider.base_url is required")
	}
	if c.Provider.APIKeyEnv == "" {
		return fmt.Errorf("provider.api_key_env is required")
	}
	if c.Provider.Type != DefaultProviderType {
		return fmt.Errorf("provider.type %q is unsupported in v1", c.Provider.Type)
	}
	return nil
}

func (c Config) RuntimeStatePath() string {
	return filepath.Join(c.Runtime.HostStateDir, "runtime-instance.json")
}

func resolvePath(baseDir string, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}
