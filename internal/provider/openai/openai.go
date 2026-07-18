// Package openai is a hand-rolled client for the OpenAI-compatible Chat
// Completions wire format (AGENTS.md: no SDKs) -- the format Ollama,
// llama.cpp's server, LM Studio, Nous Research's hosted API, and OpenAI
// itself all implement, so one client covers all of them via base URL.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/unhewn/hewn/internal/provider"
)

const (
	// defaultBaseURL is a local Ollama instance's OpenAI-compatible
	// endpoint, this session's confirmed first target. Point elsewhere
	// (Nous Research, real OpenAI, llama.cpp's server) via OPENAI_BASE_URL.
	defaultBaseURL   = "http://localhost:11434/v1"
	defaultMaxTokens = 4096
)

func init() {
	provider.Register("openai", New)
}

// Client is a provider.Provider for any OpenAI-compatible backend.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New constructs the client. OPENAI_BASE_URL defaults to defaultBaseURL
// when unset. OPENAI_API_KEY is optional, unlike Anthropic's mandatory
// key: no Authorization header is sent when it's empty, since e.g. Ollama
// needs no auth at all.
func New() (provider.Provider, error) {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  os.Getenv("OPENAI_API_KEY"),
		http:    &http.Client{},
	}, nil
}

// Name returns "openai".
func (c *Client) Name() string { return "openai" }

// Models lists the models the backend currently reports via GET /models.
func (c *Client) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("openai: build models request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: models request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}
	defer resp.Body.Close()

	var out wireModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("openai: decode models response: %w", err)
	}

	models := make([]provider.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, provider.ModelInfo{ID: m.ID, DisplayName: m.ID})
	}
	return models, nil
}

// Stream starts a turn against POST /chat/completions with streaming
// enabled.
func (c *Client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	wr := toWireRequest(req)
	if wr.MaxTokens == 0 {
		wr.MaxTokens = defaultMaxTokens
	}

	body, err := json.Marshal(wr)
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build stream request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	return newChunkStream(resp.Body), nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// apiError reads and closes resp.Body, translating a non-200 response
// into an error. Callers must not also close resp.Body.
func apiError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	var wireErr wireAPIError
	if err := json.Unmarshal(body, &wireErr); err == nil && wireErr.Error.Message != "" {
		return fmt.Errorf("openai: %s: %s", resp.Status, wireErr.Error.Message)
	}
	return fmt.Errorf("openai: %s: %s", resp.Status, string(body))
}
