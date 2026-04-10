package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/chenan/codo/internal/config"
)

type Adapter interface {
	ProviderType() string
	BuildRequest(ctx context.Context, incoming *http.Request, body []byte) (*http.Request, error)
}

type BailianOpenAICompatible struct {
	baseURL *url.URL
	apiKey  string
}

func New(cfg config.ProviderConfig) (Adapter, error) {
	switch cfg.Type {
	case config.DefaultProviderType:
		parsed, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("parse provider.base_url: %w", err)
		}
		apiKey := os.Getenv(cfg.APIKeyEnv)
		if apiKey == "" {
			return nil, fmt.Errorf("provider api key env %q is not set", cfg.APIKeyEnv)
		}
		return &BailianOpenAICompatible{
			baseURL: parsed,
			apiKey:  apiKey,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
	}
}

func (b *BailianOpenAICompatible) ProviderType() string {
	return config.DefaultProviderType
}

func (b *BailianOpenAICompatible) BuildRequest(ctx context.Context, incoming *http.Request, body []byte) (*http.Request, error) {
	target := *b.baseURL
	target.Path = joinURLPath(b.baseURL.Path, incoming.URL.Path)
	target.RawQuery = incoming.URL.RawQuery

	req, err := http.NewRequestWithContext(ctx, incoming.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	copyHeader(req.Header, incoming.Header, "Content-Type", "Accept")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	return req, nil
}

func ExtractTargetModel(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	model, _ := payload["model"].(string)
	return model
}

func ReadAllBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return payload, nil
}

func copyHeader(dst http.Header, src http.Header, keys ...string) {
	for _, key := range keys {
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
}

func joinURLPath(basePath string, requestPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	if strings.HasSuffix(basePath, "/v1") && strings.HasPrefix(requestPath, "/v1/") {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	requestPath = strings.TrimLeft(requestPath, "/")
	if basePath == "" {
		return "/" + requestPath
	}
	if requestPath == "" {
		return basePath
	}
	return basePath + "/" + requestPath
}
