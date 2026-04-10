package controlplane

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chenan/codo/internal/config"
	"github.com/chenan/codo/internal/jsonl"
	"github.com/chenan/codo/internal/provider"
	"github.com/chenan/codo/internal/transport"
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

	auditHTTP := &http.Server{Handler: withHealthz(auditCollector.Handler())}
	proxyHTTP := &http.Server{Handler: withHealthz(proxyServer.Handler())}

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

func CheckHealth(ctx context.Context, cfg config.Config) error {
	if err := checkSocketHealth(ctx, cfg.Audit.SocketPath); err != nil {
		return fmt.Errorf("audit socket unhealthy: %w", err)
	}
	if err := checkSocketHealth(ctx, cfg.Proxy.SocketPath); err != nil {
		return fmt.Errorf("proxy socket unhealthy: %w", err)
	}
	return nil
}

func checkSocketHealth(ctx context.Context, socketPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	resp, err := transport.NewUnixHTTPClient(socketPath).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
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

func withHealthz(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", next)
	return mux
}
