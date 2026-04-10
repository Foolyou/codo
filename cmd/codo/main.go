package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/chenan/codo/internal/bootstrap"
	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/controlplane"
	codoruntime "github.com/chenan/codo/internal/runtime"
)

var errUsage = errors.New("usage")

const configFlagUsage = "Path to runtime config JSON (default: CODO_CONFIG or ~/.codo/config/runtime.json)"

type cli struct {
	up                func(context.Context, string) error
	loadConfig        func(string) (config.Config, error)
	serveControlPlane func(context.Context, config.Config) error
	buildRuntimeImage func(context.Context, config.Config) error
	startRuntime      func(context.Context, config.Config) error
	stopRuntime       func(context.Context, config.Config) error
	rebuildRuntime    func(context.Context, config.Config) error
	runtimeStatus     func(context.Context, config.Config) error
	execInRuntime     func(context.Context, config.Config, string, string, string) error
	reconnectRuntime  func(context.Context, config.Config, string) error
	runAuditedBash    func(context.Context, string) error
	proxyRequest      func(context.Context, string, string, []byte) error
}

func defaultCLI() cli {
	return cli{
		up:                bootstrap.Up,
		loadConfig:        loadConfig,
		serveControlPlane: controlplane.Serve,
		buildRuntimeImage: codoruntime.BuildRuntimeImage,
		startRuntime:      codoruntime.EnsureRuntimeStarted,
		stopRuntime:       codoruntime.StopRuntime,
		rebuildRuntime:    codoruntime.RebuildRuntime,
		runtimeStatus:     codoruntime.RuntimeStatus,
		execInRuntime:     codoruntime.ExecInRuntime,
		reconnectRuntime:  codoruntime.ReconnectRuntime,
		runAuditedBash:    codoruntime.RunAuditedBash,
		proxyRequest:      codoruntime.ProxyRequest,
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	if err := defaultCLI().run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			usage()
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func loadConfig(explicitConfigPath string) (config.Config, error) {
	cfg, _, err := config.LoadResolved(explicitConfigPath)
	return cfg, err
}

func (c cli) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch args[0] {
	case "up":
		return c.runUp(ctx, args[1:])
	case "control-plane":
		return c.runControlPlane(ctx, args[1:])
	case "runtime":
		return c.runRuntime(ctx, args[1:])
	default:
		return errUsage
	}
}

func (c cli) runUp(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	var configPath string
	fs.StringVar(&configPath, "config", "", configFlagUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: codo up [--config <path>]")
	}
	return c.up(ctx, configPath)
}

func (c cli) runControlPlane(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("%w: codo control-plane serve [--config <path>]", errUsage)
	}
	fs := flag.NewFlagSet("control-plane serve", flag.ContinueOnError)
	var configPath string
	fs.StringVar(&configPath, "config", "", configFlagUsage)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := c.loadConfig(configPath)
	if err != nil {
		return err
	}
	return c.serveControlPlane(ctx, cfg)
}

func (c cli) runRuntime(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: codo runtime <build-image|start|stop|rebuild|status|exec|reconnect|shell|bash|proxy-request>", errUsage)
	}

	switch args[0] {
	case "build-image":
		return c.withConfig(args[1:], func(cfg config.Config) error {
			return c.buildRuntimeImage(ctx, cfg)
		})
	case "start":
		return c.withConfig(args[1:], func(cfg config.Config) error {
			return c.startRuntime(ctx, cfg)
		})
	case "stop":
		return c.withConfig(args[1:], func(cfg config.Config) error {
			return c.stopRuntime(ctx, cfg)
		})
	case "rebuild":
		return c.withConfig(args[1:], func(cfg config.Config) error {
			return c.rebuildRuntime(ctx, cfg)
		})
	case "status":
		return c.withConfig(args[1:], func(cfg config.Config) error {
			return c.runtimeStatus(ctx, cfg)
		})
	case "exec":
		return c.runtimeExec(ctx, args[1:])
	case "reconnect", "shell":
		return c.runtimeReconnect(ctx, args[1:])
	case "bash":
		return c.runtimeBash(ctx, args[1:])
	case "proxy-request":
		return c.runtimeProxyRequest(ctx, args[1:])
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

func (c cli) runtimeExec(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runtime exec", flag.ContinueOnError)
	var configPath string
	var sessionID string
	var workdir string
	fs.StringVar(&configPath, "config", "", configFlagUsage)
	fs.StringVar(&sessionID, "session-id", "", "Optional session ID override")
	fs.StringVar(&workdir, "workdir", config.DefaultWorkspaceMountPath, "Working directory inside container")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := strings.Join(fs.Args(), " ")
	if command == "" {
		return fmt.Errorf("runtime exec requires a shell command after flags")
	}
	cfg, err := c.loadConfig(configPath)
	if err != nil {
		return err
	}
	return c.execInRuntime(ctx, cfg, sessionID, workdir, command)
}

func (c cli) runtimeReconnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runtime reconnect", flag.ContinueOnError)
	var configPath string
	var sessionID string
	fs.StringVar(&configPath, "config", "", configFlagUsage)
	fs.StringVar(&sessionID, "session-id", "", "Optional session ID override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := c.loadConfig(configPath)
	if err != nil {
		return err
	}
	return c.reconnectRuntime(ctx, cfg, sessionID)
}

func (c cli) runtimeBash(ctx context.Context, args []string) error {
	command := strings.Join(args, " ")
	command = strings.TrimPrefix(command, "-- ")
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("runtime bash requires a command string")
	}
	return c.runAuditedBash(ctx, command)
}

func (c cli) runtimeProxyRequest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runtime proxy-request", flag.ContinueOnError)
	var method string
	var path string
	var bodyFile string
	fs.StringVar(&method, "method", "POST", "HTTP method")
	fs.StringVar(&path, "path", "/v1/chat/completions", "Proxy path")
	fs.StringVar(&bodyFile, "body-file", "", "Optional path to request body file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var body []byte
	if bodyFile != "" {
		raw, err := os.ReadFile(bodyFile)
		if err != nil {
			return fmt.Errorf("read body file: %w", err)
		}
		body = raw
	}
	return c.proxyRequest(ctx, method, path, body)
}

func (c cli) withConfig(args []string, fn func(config.Config) error) error {
	fs := flag.NewFlagSet("runtime-config", flag.ContinueOnError)
	var configPath string
	fs.StringVar(&configPath, "config", "", configFlagUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := c.loadConfig(configPath)
	if err != nil {
		return err
	}
	return fn(cfg)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: codo <up|control-plane|runtime> ...")
}
