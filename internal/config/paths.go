package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvCodoHome   = "CODO_HOME"
	EnvCodoConfig = "CODO_CONFIG"

	DefaultCodoHomeDirName = ".codo"
	DefaultConfigDirName   = "config"
	DefaultConfigFileName  = "runtime.json"
	DefaultWorkspaceDir    = "workspace"
	DefaultStateDir        = "state"
)

type ConfigPathSource string

const (
	ConfigPathFromFlag    ConfigPathSource = "flag"
	ConfigPathFromEnv     ConfigPathSource = "env"
	ConfigPathFromDefault ConfigPathSource = "default"
)

type ResolvedConfigPath struct {
	Path    string
	HomeDir string
	Source  ConfigPathSource
}

func (r ResolvedConfigPath) IsDefault() bool {
	return r.Source == ConfigPathFromDefault
}

func ResolveCodoHome() (string, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvCodoHome)); raw != "" {
		return resolveAbsolutePath(raw)
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	if userHome == "" {
		return "", fmt.Errorf("resolve user home: empty path")
	}
	return filepath.Join(userHome, DefaultCodoHomeDirName), nil
}

func DefaultConfigPath(homeDir string) string {
	return filepath.Join(homeDir, DefaultConfigDirName, DefaultConfigFileName)
}

func ResolveConfigPath(explicit string) (ResolvedConfigPath, error) {
	if raw := strings.TrimSpace(explicit); raw != "" {
		path, err := resolveAbsolutePath(raw)
		if err != nil {
			return ResolvedConfigPath{}, err
		}
		return ResolvedConfigPath{
			Path:   path,
			Source: ConfigPathFromFlag,
		}, nil
	}

	if raw := strings.TrimSpace(os.Getenv(EnvCodoConfig)); raw != "" {
		path, err := resolveAbsolutePath(raw)
		if err != nil {
			return ResolvedConfigPath{}, err
		}
		return ResolvedConfigPath{
			Path:   path,
			Source: ConfigPathFromEnv,
		}, nil
	}

	homeDir, err := ResolveCodoHome()
	if err != nil {
		return ResolvedConfigPath{}, err
	}
	return ResolvedConfigPath{
		Path:    DefaultConfigPath(homeDir),
		HomeDir: homeDir,
		Source:  ConfigPathFromDefault,
	}, nil
}

func LoadResolved(explicit string) (Config, ResolvedConfigPath, error) {
	resolved, err := ResolveConfigPath(explicit)
	if err != nil {
		return Config{}, ResolvedConfigPath{}, err
	}

	cfg, err := Load(resolved.Path)
	if err != nil {
		return Config{}, resolved, err
	}
	return cfg, resolved, nil
}

func EnsureDefaultHomeConfig(resolved ResolvedConfigPath) (bool, error) {
	if !resolved.IsDefault() {
		return false, nil
	}
	if resolved.Path == "" {
		return false, fmt.Errorf("default config path is empty")
	}
	if resolved.HomeDir == "" {
		return false, fmt.Errorf("default home dir is empty")
	}

	if _, err := os.Stat(resolved.Path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat default config: %w", err)
	}

	configDir := filepath.Dir(resolved.Path)
	workspaceDir := filepath.Join(resolved.HomeDir, DefaultWorkspaceDir)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return false, fmt.Errorf("create workspace dir: %w", err)
	}

	contents, err := defaultConfigFile()
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(resolved.Path, contents, 0o644); err != nil {
		return false, fmt.Errorf("write default config: %w", err)
	}
	return true, nil
}

func resolveAbsolutePath(value string) (string, error) {
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return filepath.Clean(path), nil
}

func defaultConfigFile() ([]byte, error) {
	type fileConfig struct {
		Runtime struct {
			Name                string `json:"name"`
			Image               string `json:"image"`
			WorkspacePath       string `json:"workspace_path"`
			WorkspaceLabel      string `json:"workspace_label"`
			WorkspaceMountPath  string `json:"workspace_mount_path"`
			HostStateDir        string `json:"host_state_dir"`
			ContainerControlDir string `json:"container_control_dir"`
		} `json:"runtime"`
		Provider struct {
			Type      string `json:"type"`
			BaseURL   string `json:"base_url"`
			APIKeyEnv string `json:"api_key_env"`
		} `json:"provider"`
		Audit struct {
			PreviewBytes int `json:"preview_bytes"`
		} `json:"audit"`
	}

	var cfg fileConfig
	cfg.Runtime.Name = DefaultRuntimeName
	cfg.Runtime.Image = DefaultImage
	cfg.Runtime.WorkspacePath = filepath.ToSlash(filepath.Join("..", DefaultWorkspaceDir))
	cfg.Runtime.WorkspaceLabel = DefaultWorkspaceDir
	cfg.Runtime.WorkspaceMountPath = DefaultWorkspaceMountPath
	cfg.Runtime.HostStateDir = DefaultStateDir
	cfg.Runtime.ContainerControlDir = DefaultContainerControlDir
	cfg.Provider.Type = DefaultProviderType
	cfg.Provider.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	cfg.Provider.APIKeyEnv = "BAILIAN_API_KEY"
	cfg.Audit.PreviewBytes = DefaultPreviewBytes

	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal default config: %w", err)
	}
	return append(encoded, '\n'), nil
}
