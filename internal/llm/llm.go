// Package llm talks to Groq's OpenAI-compatible chat API using only the standard library.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client is what the rest of the code needs from a language model.
// Tests replace it with a fake.
type Client interface {
	// JSON asks for a reply that must match schema and returns the raw JSON.
	JSON(ctx context.Context, system, user, name string, schema map[string]any) ([]byte, error)
	// Text asks for a free-text reply.
	Text(ctx context.Context, system, user string) (string, error)
}

// Groq is the production Client.
type Groq struct {
	APIKey          string
	BaseURL         string
	Model           string
	GradeMaxTokens  int
	AnswerMaxTokens int
	MaxRetries      int
	HTTP            *http.Client
	// Sleep is replaceable so retry tests do not wait.
	Sleep func(context.Context, time.Duration) error
}

// NewGroq returns a client with a 60 second request timeout.
func NewGroq(apiKey, baseURL, model string, gradeTokens, answerTokens, retries int) *Groq {
	return &Groq{
		APIKey: apiKey, BaseURL: baseURL, Model: model,
		GradeMaxTokens: gradeTokens, AnswerMaxTokens: answerTokens, MaxRetries: retries,
		HTTP: &http.Client{Timeout: 60 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string    `json:"model"`
	Messages       []message `json:"messages"`
	Temperature    float64   `json:"temperature"`
	MaxTokens      int       `json:"max_completion_tokens"`
	ResponseFormat any       `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// JSON implements Client. Strict mode makes Groq enforce the schema.
func (g *Groq) JSON(ctx context.Context, system, user, name string, schema map[string]any) ([]byte, error) {
	out, err := g.chat(ctx, chatRequest{
		Model:       g.Model,
		Messages:    []message{{"system", system}, {"user", user}},
		Temperature: 0,
		MaxTokens:   g.GradeMaxTokens,
		ResponseFormat: map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": name, "strict": true, "schema": schema},
		},
	})
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, errors.New("model returned an empty reply (token budget spent on reasoning?)")
	}
	return []byte(out), nil
}

// Text implements Client.
func (g *Groq) Text(ctx context.Context, system, user string) (string, error) {
	return g.chat(ctx, chatRequest{
		Model:       g.Model,
		Messages:    []message{{"system", system}, {"user", user}},
		Temperature: 0,
		MaxTokens:   g.AnswerMaxTokens,
	})
}

// APIError is a non-2xx reply from the API.
type APIError struct {
	Status     int
	Body       string
	retryAfter string // Retry-After header, if the API sent one
}

func (e *APIError) Error() string { return fmt.Sprintf("groq api status %d: %s", e.Status, e.Body) }

func retryable(status int) bool { return status == 429 || status >= 500 }

func (g *Groq) chat(ctx context.Context, req chatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 0; attempt <= g.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := g.sleep(ctx, backoff(attempt, lastErr)); err != nil {
				return "", err
			}
		}
		content, err := g.once(ctx, body)
		if err == nil {
			return content, nil
		}
		lastErr = err
		var apiErr *APIError
		switch {
		case errors.As(err, &apiErr) && !retryable(apiErr.Status):
			return "", err
		case ctx.Err() != nil:
			return "", ctx.Err()
		}
		slog.Warn("groq call failed, will retry", "attempt", attempt+1, "err", err)
	}
	return "", fmt.Errorf("groq: giving up after %d retries: %w", g.MaxRetries, lastErr)
}

func (g *Groq) once(ctx context.Context, body []byte) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTP.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", &APIError{Status: resp.StatusCode, Body: truncate(string(raw), 300), retryAfter: resp.Header.Get("Retry-After")}
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode groq reply: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("groq reply had no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func (g *Groq) sleep(ctx context.Context, d time.Duration) error {
	if g.Sleep != nil {
		return g.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// backoff honours Retry-After when Groq sends it, else exponential with jitter.
func backoff(attempt int, last error) time.Duration {
	var apiErr *APIError
	if errors.As(last, &apiErr) && apiErr.retryAfter != "" {
		if secs, err := strconv.ParseFloat(apiErr.retryAfter, 64); err == nil && secs > 0 && secs < 120 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	base := time.Duration(1<<min(attempt, 5)) * 500 * time.Millisecond
	return base + time.Duration(rand.Int64N(int64(base/2)))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
