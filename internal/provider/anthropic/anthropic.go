// Package anthropic is a hand-rolled client for the Anthropic Messages API:
// SSE streaming, tool use, and nothing else. No SDK (AGENTS.md dependency
// rule) — the wire format is simple and we want direct control over
// streaming and tool-call deltas.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/unhewn/hewn/internal/provider"
)

const (
	messagesURL      = "https://api.anthropic.com/v1/messages"
	modelsURL        = "https://api.anthropic.com/v1/models"
	countTokensURL   = "https://api.anthropic.com/v1/messages/count_tokens" //nolint:gosec // URL, not a credential
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 4096
)

func init() {
	provider.Register("anthropic", New)
}

// Client is a provider.Provider backed by the Anthropic Messages API.
type Client struct {
	apiKey string
	http   *http.Client
}

// New constructs the Anthropic provider, reading its credential from
// ANTHROPIC_API_KEY.
func New() (provider.Provider, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, provider.ErrNoAPIKey
	}
	return &Client{apiKey: key, http: &http.Client{}}, nil
}

// Name returns "anthropic".
func (c *Client) Name() string { return "anthropic" }

// Models lists the models available from the Anthropic API.
func (c *Client) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build models request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: models request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}
	defer resp.Body.Close()

	var out wireModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("anthropic: decode models response: %w", err)
	}

	models := make([]provider.ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, provider.ModelInfo{ID: m.ID, DisplayName: m.DisplayName})
	}
	return models, nil
}

// Stream starts a turn against the Messages API with streaming enabled.
func (c *Client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	wr, err := toWireRequest(req)
	if err != nil {
		return nil, err
	}
	if wr.MaxTokens == 0 {
		wr.MaxTokens = defaultMaxTokens
	}

	body, err := json.Marshal(wr)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build stream request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: stream request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	return newSSEStream(resp.Body), nil
}

// CountTokens calls the real /v1/messages/count_tokens endpoint: the
// exact input-token count for req, at no cost and without generating a
// response. Tools and system count toward the total the same as they
// would in the real Stream call.
func (c *Client) CountTokens(ctx context.Context, req provider.Request) (int, error) {
	wr, err := toWireRequest(req)
	if err != nil {
		return 0, err
	}

	body, err := json.Marshal(wireCountTokensRequest{
		Model:    wr.Model,
		System:   wr.System,
		Messages: wr.Messages,
		Tools:    wr.Tools,
	})
	if err != nil {
		return 0, fmt.Errorf("anthropic: encode count_tokens request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, countTokensURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("anthropic: build count_tokens request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("anthropic: count_tokens request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, apiError(resp)
	}
	defer resp.Body.Close()

	var out wireCountTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("anthropic: decode count_tokens response: %w", err)
	}
	return out.InputTokens, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// apiError reads and closes resp.Body, translating a non-200 response into
// an error. Callers must not also close resp.Body.
func apiError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	var wireErr wireAPIError
	if err := json.Unmarshal(body, &wireErr); err == nil && wireErr.Error.Message != "" {
		return fmt.Errorf("anthropic: %s: %s", resp.Status, wireErr.Error.Message)
	}
	return fmt.Errorf("anthropic: %s", resp.Status)
}
