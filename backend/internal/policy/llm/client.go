package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// MaxResponseBytes caps the provider response body.
const MaxResponseBytes = 1 << 20

// Typed errors. Messages never contain the API key or the request URL.
var (
	ErrNotConfigured   = errors.New("llm: not configured")
	ErrTimeout         = errors.New("llm: request timed out")
	ErrHTTP            = errors.New("llm: provider request failed")
	ErrInvalidJSON     = errors.New("llm: invalid JSON from provider")
	ErrSchemaViolation = errors.New("llm: proposal violates the schema")
)

// HTTPError carries the provider status (0 for a transport failure). It
// matches ErrHTTP under errors.Is.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string {
	if e.Status == 0 {
		return "llm: provider request failed (transport error)"
	}
	return fmt.Sprintf("llm: provider request failed (status %d)", e.Status)
}

// Is makes errors.Is(err, ErrHTTP) true.
func (e *HTTPError) Is(target error) bool { return target == ErrHTTP }

// Client calls an OpenAI-compatible chat completions endpoint.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a Client. httpClient may be nil; its Timeout is set from
// cfg.RequestTimeout on a copy.
func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	if !cfg.Available() {
		return nil, ErrNotConfigured
	}
	hc := &http.Client{}
	if httpClient != nil {
		cp := *httpClient
		hc = &cp
	}
	hc.Timeout = cfg.RequestTimeout
	// A provider must not redirect the bearer token elsewhere.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{cfg: cfg, http: hc}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    int            `json:"temperature"`
	MaxTokens      int            `json:"max_tokens"`
	ResponseFormat responseFormat `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete sends one system and one user message and returns the assistant
// content and the token usage. Usage is returned (when the provider sent a
// well-formed response) even alongside ErrInvalidJSON. When the provider
// reports no usage, the per-call completion cap is charged conservatively.
func (c *Client) Complete(ctx context.Context, system, user string) (string, Usage, error) {
	body, err := json.Marshal(chatRequest{
		Model:          c.cfg.Model,
		Messages:       []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature:    0,
		MaxTokens:      c.cfg.MaxTokensPerCall,
		ResponseFormat: responseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", Usage{}, fmt.Errorf("%w: encode request", ErrHTTP)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, &HTTPError{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if c.cfg.HTTPReferer != "" {
		req.Header.Set("HTTP-Referer", c.cfg.HTTPReferer)
	}
	if c.cfg.AppTitle != "" {
		req.Header.Set("X-Title", c.cfg.AppTitle)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// err text embeds the URL; report only the class of failure.
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return "", Usage{}, ErrTimeout
		}
		if errors.Is(err, context.Canceled) {
			return "", Usage{}, fmt.Errorf("%w: canceled", ErrHTTP)
		}
		return "", Usage{}, &HTTPError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", Usage{}, &HTTPError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return "", Usage{}, ErrTimeout
		}
		return "", Usage{}, &HTTPError{}
	}
	if len(raw) > MaxResponseBytes {
		return "", Usage{}, fmt.Errorf("%w: response exceeds %d bytes", ErrInvalidJSON, MaxResponseBytes)
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", Usage{}, fmt.Errorf("%w: response envelope", ErrInvalidJSON)
	}
	u := Usage{}
	if cr.Usage != nil {
		u.PromptTokens, u.CompletionTokens, u.TotalTokens = cr.Usage.PromptTokens, cr.Usage.CompletionTokens, cr.Usage.TotalTokens
		if u.TotalTokens == 0 {
			u.TotalTokens = u.PromptTokens + u.CompletionTokens
		}
	}
	if cr.Usage == nil || u.TotalTokens == 0 {
		u.CompletionTokens = c.cfg.MaxTokensPerCall
		u.TotalTokens = c.cfg.MaxTokensPerCall
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", u, fmt.Errorf("%w: no content", ErrInvalidJSON)
	}
	return stripFence(cr.Choices[0].Message.Content), u, nil
}

// stripFence removes a surrounding ```json code fence some providers add.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimPrefix(t, "json")
	t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	return strings.TrimSpace(t)
}
