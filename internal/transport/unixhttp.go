package transport

import (
	"context"
	"net"
	"net/http"
	"time"
)

func NewUnixHTTPClient(socketPath string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}
}
