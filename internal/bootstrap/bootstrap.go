package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/controlplane"
	codoruntime "github.com/chenan/codo/internal/runtime"
)

type dependencies struct {
	resolveConfigPath   func(string) (config.ResolvedConfigPath, error)
	ensureDefaultConfig func(config.ResolvedConfigPath) (bool, error)
	loadConfig          func(string) (config.Config, error)
	stat                func(string) (os.FileInfo, error)
	ensureImage         func(context.Context, config.Config) error
	serveControlPlane   func(context.Context, config.Config, chan<- struct{}) error
	ensureRuntime       func(context.Context, config.Config) error
}

func defaultDependencies() dependencies {
	return dependencies{
		resolveConfigPath:   config.ResolveConfigPath,
		ensureDefaultConfig: config.EnsureDefaultHomeConfig,
		loadConfig:          config.Load,
		stat:                os.Stat,
		ensureImage:         codoruntime.EnsureRuntimeImageAvailable,
		serveControlPlane:   controlplane.ServeWithReady,
		ensureRuntime:       codoruntime.EnsureRuntimeStarted,
	}
}

func Up(ctx context.Context, explicitConfigPath string) error {
	return runUp(ctx, explicitConfigPath, defaultDependencies())
}

func runUp(ctx context.Context, explicitConfigPath string, deps dependencies) error {
	resolved, err := deps.resolveConfigPath(explicitConfigPath)
	if err != nil {
		return err
	}

	if resolved.IsDefault() {
		if _, err := deps.ensureDefaultConfig(resolved); err != nil {
			return err
		}
	} else if _, err := deps.stat(resolved.Path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file does not exist: %s", resolved.Path)
		}
		return fmt.Errorf("stat config file: %w", err)
	}

	cfg, err := deps.loadConfig(resolved.Path)
	if err != nil {
		return err
	}
	if err := deps.ensureImage(ctx, cfg); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	readyCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- deps.serveControlPlane(runCtx, cfg, readyCh)
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return nil
	}

	if err := deps.ensureRuntime(runCtx, cfg); err != nil {
		return err
	}

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}
