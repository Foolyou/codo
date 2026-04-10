package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/controlplane"
	codoruntime "github.com/chenan/codo/internal/runtime"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "control-plane":
		err = runControlPlane(ctx, os.Args[2:])
	case "runtime":
		err = runRuntime(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runControlPlane(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: codo control-plane serve --config <path>")
	}
	fs := flag.NewFlagSet("control-plane serve", flag.ContinueOnError)
	var configPath string
	fs.StringVar(&configPath, "config", "examples/runtime-config.example.json", "Path to runtime config JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return controlplane.Serve(ctx, cfg)
}

func runRuntime(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: codo runtime <build-image|start|stop|rebuild|status|exec|reconnect|shell|bash|proxy-request>")
	}
	switch args[0] {
	case "build-image":
		return withConfig(args[1:], func(cfg config.Config) error {
			return codoruntime.BuildRuntimeImage(ctx, cfg)
		})
	case "start":
		return withConfig(args[1:], func(cfg config.Config) error {
			return codoruntime.EnsureRuntimeStarted(ctx, cfg)
		})
	case "stop":
		return withConfig(args[1:], func(cfg config.Config) error {
			return codoruntime.StopRuntime(ctx, cfg)
		})
	case "rebuild":
		return withConfig(args[1:], func(cfg config.Config) error {
			return codoruntime.RebuildRuntime(ctx, cfg)
		})
	case "status":
		return withConfig(args[1:], func(cfg config.Config) error {
			return codoruntime.RuntimeStatus(ctx, cfg)
		})
	case "exec":
		return runtimeExec(ctx, args[1:])
	case "reconnect", "shell":
		return runtimeReconnect(ctx, args[1:])
	case "bash":
		return runtimeBash(ctx, args[1:])
	case "proxy-request":
		return runtimeProxyRequest(ctx, args[1:])
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

func runtimeExec(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runtime exec", flag.ContinueOnError)
	var configPath string
	var sessionID string
	var workdir string
	fs.StringVar(&configPath, "config", "examples/runtime-config.example.json", "Path to runtime config JSON")
	fs.StringVar(&sessionID, "session-id", "", "Optional session ID override")
	fs.StringVar(&workdir, "workdir", config.DefaultWorkspaceMountPath, "Working directory inside container")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := strings.Join(fs.Args(), " ")
	if command == "" {
		return fmt.Errorf("runtime exec requires a shell command after flags")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return codoruntime.ExecInRuntime(ctx, cfg, sessionID, workdir, command)
}

func runtimeReconnect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runtime reconnect", flag.ContinueOnError)
	var configPath string
	var sessionID string
	fs.StringVar(&configPath, "config", "examples/runtime-config.example.json", "Path to runtime config JSON")
	fs.StringVar(&sessionID, "session-id", "", "Optional session ID override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return codoruntime.ReconnectRuntime(ctx, cfg, sessionID)
}

func runtimeBash(ctx context.Context, args []string) error {
	command := strings.Join(args, " ")
	command = strings.TrimPrefix(command, "-- ")
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("runtime bash requires a command string")
	}
	return codoruntime.RunAuditedBash(ctx, command)
}

func runtimeProxyRequest(ctx context.Context, args []string) error {
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
	return codoruntime.ProxyRequest(ctx, method, path, body)
}

func withConfig(args []string, fn func(cfg config.Config) error) error {
	fs := flag.NewFlagSet("runtime-config", flag.ContinueOnError)
	var configPath string
	fs.StringVar(&configPath, "config", "examples/runtime-config.example.json", "Path to runtime config JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return fn(cfg)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: codo <control-plane|runtime> ...")
}
