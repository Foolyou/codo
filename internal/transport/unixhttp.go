package transport

import (
	"context"
	"net"
	"net/http"
	"time"
)

func NewUnixHTTPClient(socketPath string) *http.Client {
	return newUnixHTTPClient(socketPath, 60*time.Second)
}

func NewStreamingUnixHTTPClient(socketPath string) *http.Client {
	return newUnixHTTPClient(socketPath, 0)
}

func newUnixHTTPClient(socketPath string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}

	client := &http.Client{
		Transport: transport,
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client
}
