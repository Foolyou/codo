package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/ids"
)

var ErrAssistantRuntimeOutOfDate = errors.New("runtime container codo binary is incompatible with assistant chat")

type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type ContainerSpec struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Env     map[string]string `json:"env"`
	Labels  map[string]string `json:"labels"`
	Mounts  []Mount           `json:"mounts"`
	Command []string          `json:"command"`
}

func BuildContainerSpec(cfg config.Config, state State) ContainerSpec {
	return ContainerSpec{
		Name:  state.ContainerName,
		Image: cfg.Runtime.Image,
		Env: map[string]string{
			EnvRuntimeInstanceID:  state.RuntimeInstanceID,
			EnvWorkspaceID:        cfg.Runtime.WorkspaceLabel,
			EnvWorkspacePathLabel: cfg.Runtime.WorkspaceLabel,
			EnvWorkspaceMountPath: cfg.Runtime.WorkspaceMountPath,
			EnvAuditSocket:        filepath.Join(cfg.Runtime.ContainerControlDir, filepath.Base(cfg.Audit.SocketPath)),
			EnvAuditPreviewBytes:  strconv.Itoa(cfg.Audit.PreviewBytes),
			EnvModelProxySocket:   filepath.Join(cfg.Runtime.ContainerControlDir, filepath.Base(cfg.Proxy.SocketPath)),
		},
		Labels: map[string]string{
			"codo.runtime_instance_id": state.RuntimeInstanceID,
			"codo.workspace_path":      cfg.Runtime.WorkspacePath,
			"codo.workspace_label":     cfg.Runtime.WorkspaceLabel,
		},
		Mounts: []Mount{
			{
				Source: cfg.Runtime.WorkspacePath,
				Target: cfg.Runtime.WorkspaceMountPath,
			},
			{
				Source: cfg.Runtime.HostControlDir,
				Target: cfg.Runtime.ContainerControlDir,
			},
		},
		Command: []string{"sleep", "infinity"},
	}
}

func BuildDockerRunArgs(spec ContainerSpec) []string {
	args := []string{"run", "-d", "--name", spec.Name, "--restart", "unless-stopped"}
	for _, key := range sortedKeys(spec.Labels) {
		value := spec.Labels[key]
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	for _, key := range sortedKeys(spec.Env) {
		value := spec.Env[key]
		args = append(args, "--env", fmt.Sprintf("%s=%s", key, value))
	}
	for _, mount := range spec.Mounts {
		mountArg := fmt.Sprintf("type=bind,src=%s,dst=%s", mount.Source, mount.Target)
		if mount.ReadOnly {
			mountArg += ",readonly"
		}
		args = append(args, "--mount", mountArg)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func BuildDockerExecArgs(containerName string, sessionID string, workdir string, command []string, tty bool) []string {
	args := []string{"exec"}
	if tty {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}
	if workdir != "" {
		args = append(args, "-w", workdir)
	}
	if sessionID != "" {
		args = append(args, "-e", fmt.Sprintf("%s=%s", EnvSessionID, sessionID))
	}
	args = append(args, containerName)
	args = append(args, command...)
	return args
}

func EnsureRuntimeStarted(ctx context.Context, cfg config.Config) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}
	spec := BuildContainerSpec(cfg, state)

	inspect := exec.CommandContext(ctx, "docker", "inspect", spec.Name)
	if err := inspect.Run(); err == nil {
		start := exec.CommandContext(ctx, "docker", "start", spec.Name)
		start.Stdout = os.Stdout
		start.Stderr = os.Stderr
		if err := start.Run(); err != nil {
			return fmt.Errorf("start existing container: %w", err)
		}
		return nil
	}

	if err := os.MkdirAll(cfg.Runtime.HostControlDir, 0o755); err != nil {
		return fmt.Errorf("create host control dir: %w", err)
	}

	runArgs := BuildDockerRunArgs(spec)
	cmd := exec.CommandContext(ctx, "docker", runArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create runtime container: %w", err)
	}
	return nil
}

func StopRuntime(ctx context.Context, cfg config.Config) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "stop", state.ContainerName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stop runtime container: %w", err)
	}
	return nil
}

func RebuildRuntime(ctx context.Context, cfg config.Config) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}

	rm := exec.CommandContext(ctx, "docker", "rm", "-f", state.ContainerName)
	rm.Stdout = os.Stdout
	rm.Stderr = os.Stderr
	_ = rm.Run()

	spec := BuildContainerSpec(cfg, state)
	cmd := exec.CommandContext(ctx, "docker", BuildDockerRunArgs(spec)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rebuild runtime container: %w", err)
	}
	return nil
}

func BuildRuntimeImage(ctx context.Context, cfg config.Config) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", cfg.Runtime.Image, ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build runtime image: %w", err)
	}
	return nil
}

func RuntimeImageAvailable(ctx context.Context, image string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && strings.Contains(string(output), "No such image") {
		return false, nil
	}
	return false, fmt.Errorf("inspect runtime image: %w", err)
}

func EnsureRuntimeImageAvailable(ctx context.Context, cfg config.Config) error {
	available, err := RuntimeImageAvailable(ctx, cfg.Runtime.Image)
	if err != nil {
		return err
	}
	if available {
		return nil
	}
	return BuildRuntimeImage(ctx, cfg)
}

func ExecInRuntime(ctx context.Context, cfg config.Config, sessionID string, workdir string, shellCommand string) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}
	if sessionID == "" {
		sessionID = ids.NewSessionID()
	}
	args := BuildDockerExecArgs(
		state.ContainerName,
		sessionID,
		workdir,
		[]string{"codo", "runtime", "bash", "--", shellCommand},
		false,
	)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec command inside runtime: %w", err)
	}
	return nil
}

func ReconnectRuntime(ctx context.Context, cfg config.Config, sessionID string) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}
	if sessionID == "" {
		sessionID = ids.NewSessionID()
	}
	args := BuildDockerExecArgs(
		state.ContainerName,
		sessionID,
		cfg.Runtime.WorkspaceMountPath,
		[]string{"bash"},
		true,
	)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reconnect to runtime shell: %w", err)
	}
	return nil
}

func ProbeAssistantREPL(ctx context.Context, cfg config.Config, opts AssistantREPLOptions) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}

	normalized, err := normalizeAssistantREPLOptions(opts)
	if err != nil {
		return err
	}

	args := BuildDockerExecArgs(
		state.ContainerName,
		normalized.SessionID,
		cfg.Runtime.WorkspaceMountPath,
		buildAssistantReplCommand(normalized, cfg.Runtime.WorkspaceMountPath),
		false,
	)
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
		if assistantRuntimeOutOfDate(output) {
			if output == "" {
				return ErrAssistantRuntimeOutOfDate
			}
			return fmt.Errorf("%w: %s", ErrAssistantRuntimeOutOfDate, output)
		}
		if output != "" {
			return fmt.Errorf("probe assistant repl in runtime: %w: %s", err, output)
		}
		return fmt.Errorf("probe assistant repl in runtime: %w", err)
	}
	return nil
}

func RuntimeStatus(ctx context.Context, cfg config.Config) error {
	state, _, err := LoadOrCreateState(cfg.RuntimeStatePath(), cfg.Runtime.Name)
	if err != nil {
		return err
	}

	spec := BuildContainerSpec(cfg, state)
	payload, err := json.MarshalIndent(struct {
		State State         `json:"state"`
		Spec  ContainerSpec `json:"container_spec"`
	}{
		State: state,
		Spec:  spec,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime status: %w", err)
	}
	fmt.Println(string(payload))

	inspect := exec.CommandContext(ctx, "docker", "inspect", state.ContainerName, "--format", "{{.State.Status}}")
	output, err := inspect.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && strings.Contains(string(output), "No such object") {
			fmt.Println("docker_status=missing")
			return nil
		}
		return fmt.Errorf("inspect runtime container: %w", err)
	}
	fmt.Printf("docker_status=%s\n", strings.TrimSpace(string(output)))
	return nil
}

func assistantRuntimeOutOfDate(output string) bool {
	switch {
	case strings.Contains(output, "unknown assistant subcommand"):
		return true
	case strings.Contains(output, "flag provided but not defined"):
		return true
	case strings.Contains(output, "usage: codo <control-plane|runtime> ..."):
		return true
	case strings.Contains(output, "usage: codo <up|control-plane|runtime> ..."):
		return true
	default:
		return false
	}
}
