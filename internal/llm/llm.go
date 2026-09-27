// Package llm speaks the OpenAI-compatible /v1/chat/completions API. The
// same wire format is served by OpenAI, vLLM, Ollama's /v1 endpoint, and
// Together, so the client works against all of them unchanged.
//
// The client captures prompt_tokens/completion_tokens per call, retries
// 429/5xx responses with exponential backoff, and defaults to temperature 0
// for reproducibility.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config configures a Client.
type Config struct {
	BaseURL string // e.g. https://api.openai.com/v1 (trailing path optional)
	APIKey  string
	Model   string

	// MaxRetries is the number of retries after the first attempt for
	// retryable (429/5xx) responses.
	MaxRetries int

	// Temperature defaults to 0 for reproducible output.
	Temperature float64

	// RetryBaseDelay is the initial backoff delay, doubled per retry.
	RetryBaseDelay time.Duration

	// HTTPClient overrides the default client.
	HTTPClient *http.Client
}

// Client is a minimal OpenAI-compatible chat completions client.
type Client struct {
	cfg      Config
	endpoint string
	hc       *http.Client
}

// New builds a Client. If BaseURL does not end in /chat/completions it is
// appended, so both ".../v1" and full endpoint URLs work.
func New(cfg Config) *Client {
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 500 * time.Millisecond
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	return &Client{cfg: cfg, endpoint: endpoint, hc: cfg.HTTPClient}
}

// Message is a chat message. For role "tool", ToolCallID identifies the
// assistant tool_call this message answers.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a function-calling request emitted by the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Function FunctionCall `json:"function"`
}

// FunctionCall names a tool and its JSON-encoded arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef describes one tool offered to the model (OpenAI function-calling).
type ToolDef struct {
	Type     string      `json:"type"` // always "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef holds the tool name, description, and JSON-schema parameters.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Usage reports token counts for one completion call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Response is one decoded chat completion.
type Response struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason string
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat sends one chat completion request and returns the decoded response.
// 429 and 5xx responses are retried with exponential backoff up to
// MaxRetries; other errors are returned immediately.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	delay := c.cfg.RetryBaseDelay
	for attempt := 0; ; attempt++ {
		resp, status, err := c.do(ctx, messages, tools)
		if err == nil {
			return resp, nil
		}
		// status 0 means the failure was not an HTTP status response
		// (transport or decode error); those are not retried.
		if status == 0 || (status != http.StatusTooManyRequests && status < 500) {
			return nil, err
		}
		if attempt >= c.cfg.MaxRetries {
			return nil, fmt.Errorf("%w (gave up after %d retries)", err, attempt)
		}
		if !sleepCtx(ctx, delay) {
			return nil, ctx.Err()
		}
		delay *= 2
	}
}

func (c *Client) do(ctx context.Context, messages []Message, tools []ToolDef) (*Response, int, error) {
	body, err := json.Marshal(chatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.cfg.Temperature,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	httpResp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("llm request: %w", err)
	}
	defer httpResp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("read llm response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, httpResp.StatusCode, fmt.Errorf("llm http %d: %s", httpResp.StatusCode, extractError(data))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("decode llm response: %w", err)
	}
	if cr.Error != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("llm api error: %s", cr.Error.Message)
	}

	resp := &Response{
		Usage: Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens},
	}
	if len(cr.Choices) > 0 {
		resp.Content = cr.Choices[0].Message.Content
		resp.ToolCalls = cr.Choices[0].Message.ToolCalls
		resp.FinishReason = cr.Choices[0].FinishReason
	}
	return resp, httpResp.StatusCode, nil
}

func extractError(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &e); err == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	text := strings.TrimSpace(string(data))
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	return text
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
