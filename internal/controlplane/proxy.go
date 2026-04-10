package controlplane

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chenan/codo/internal/jsonl"
	"github.com/chenan/codo/internal/protocol"
	"github.com/chenan/codo/internal/provider"
)

type ModelProxy struct {
	adapter provider.Adapter
	client  *http.Client
	writer  *jsonl.Writer
}

func NewModelProxy(adapter provider.Adapter, writer *jsonl.Writer) *ModelProxy {
	return &ModelProxy{
		adapter: adapter,
		client:  &http.Client{Timeout: 120 * time.Second},
		writer:  writer,
	}
}

func (p *ModelProxy) Handler() http.Handler {
	return http.HandlerFunc(p.handleRequest)
}

func (p *ModelProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now().UTC()
	body, err := provider.ReadAllBody(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	upstreamReq, err := p.adapter.BuildRequest(r.Context(), r, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy upstream request: %v", err), http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	copyResponseHeader(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	responseBytes, copyErr := io.Copy(w, upstreamResp.Body)
	if copyErr != nil {
		http.Error(w, fmt.Sprintf("stream upstream response: %v", copyErr), http.StatusBadGateway)
		return
	}
	_ = responseBytes

	record := protocol.ProxyAuditRecord{
		RequestID:         headerOrDefault(r.Header.Get("X-Request-ID"), startedAt),
		RequestTime:       startedAt,
		RuntimeInstanceID: r.Header.Get("X-Codo-Runtime-Instance-ID"),
		SessionID:         r.Header.Get("X-Codo-Session-ID"),
		WorkspaceID:       r.Header.Get("X-Codo-Workspace-ID"),
		Method:            r.Method,
		Path:              r.URL.Path,
		Query:             r.URL.RawQuery,
		ProviderType:      p.adapter.ProviderType(),
		TargetModel:       provider.ExtractTargetModel(body),
		ResponseStatus:    upstreamResp.StatusCode,
		DurationMillis:    time.Since(startedAt).Milliseconds(),
	}
	if err := p.writer.Append(record); err != nil {
		http.Error(w, fmt.Sprintf("persist proxy audit log: %v", err), http.StatusInternalServerError)
		return
	}
}

func copyResponseHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func headerOrDefault(value string, startedAt time.Time) string {
	if value != "" {
		return value
	}
	return fmt.Sprintf("req_%s", startedAt.Format("20060102T150405.000000000Z"))
}
