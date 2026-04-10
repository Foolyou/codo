package controlplane

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/jsonl"
	"github.com/chenan/codo/internal/provider"
)

func Serve(ctx context.Context, cfg config.Config) error {
	return ServeWithReady(ctx, cfg, nil)
}

func ServeWithReady(ctx context.Context, cfg config.Config, ready chan<- struct{}) error {
	if err := os.MkdirAll(cfg.Runtime.HostControlDir, 0o755); err != nil {
		return fmt.Errorf("create host control dir: %w", err)
	}

	adapter, err := provider.New(cfg.Provider)
	if err != nil {
		return err
	}

	auditCollector := NewAuditCollector(cfg.Audit.LogPath)
	proxyServer := NewModelProxy(adapter, jsonl.NewWriter(cfg.Proxy.AuditLogPath))

	auditListener, err := listenUnix(cfg.Audit.SocketPath)
	if err != nil {
		return fmt.Errorf("listen audit socket: %w", err)
	}
	defer auditListener.Close()

	proxyListener, err := listenUnix(cfg.Proxy.SocketPath)
	if err != nil {
		return fmt.Errorf("listen proxy socket: %w", err)
	}
	defer proxyListener.Close()

	auditHTTP := &http.Server{Handler: auditCollector.Handler()}
	proxyHTTP := &http.Server{Handler: proxyServer.Handler()}

	if ready != nil {
		close(ready)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- serveHTTP(auditHTTP, auditListener) }()
	go func() { errCh <- serveHTTP(proxyHTTP, proxyListener) }()

	select {
	case <-ctx.Done():
		_ = auditHTTP.Shutdown(context.Background())
		_ = proxyHTTP.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		_ = auditHTTP.Shutdown(context.Background())
		_ = proxyHTTP.Shutdown(context.Background())
		return err
	}
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func serveHTTP(server *http.Server, listener net.Listener) error {
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
