package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// ChatClient is the minimal chat-completions surface the loop needs. It is an
// interface so the loop can be driven by a scripted mock in tests (no API key,
// no network) and by OpenRouter in production — the same loop, so we measure the
// model, not the harness.
type ChatClient interface {
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// OpenAI-compatible wire types (OpenRouter speaks this dialect).

type ChatMessage struct {
	Role       string        `json:"role"` // system|user|assistant|tool
	Content    string        `json:"content,omitempty"`
	ToolCalls  []ToolCallMsg `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type ToolCallMsg struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded object (per OpenAI spec)
}

type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	Temperature float64       `json:"temperature"`
	TopP        float64       `json:"top_p,omitempty"`
}

type ChatResponse struct {
	Message      ChatMessage
	FinishReason string
}

// OpenRouterClient calls OpenRouter's OpenAI-compatible chat-completions API.
type OpenRouterClient struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
}

// NewOpenRouterClient reads OPENROUTER_API_KEY from the environment. It returns
// an error if the key is absent so callers fail fast before a run.
func NewOpenRouterClient() (*OpenRouterClient, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set (see issue #5); the grader and loop tests run without it")
	}
	return &OpenRouterClient{
		APIKey:  key,
		BaseURL: "https://openrouter.ai/api/v1",
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (c *OpenRouterClient) Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	// Optional attribution headers OpenRouter recommends.
	httpReq.Header.Set("HTTP-Referer", "https://github.com/pshima/sre-field-tests")
	httpReq.Header.Set("X-Title", "SRE Field Tests")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Choices []struct {
			Message      ChatMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("decode chat response (HTTP %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return ChatResponse{}, fmt.Errorf("openrouter error (HTTP %d): %s", resp.StatusCode, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("openrouter returned no choices (HTTP %d)", resp.StatusCode)
	}
	return ChatResponse{Message: parsed.Choices[0].Message, FinishReason: parsed.Choices[0].FinishReason}, nil
}
