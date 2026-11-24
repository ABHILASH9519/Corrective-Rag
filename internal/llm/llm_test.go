package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crag/internal/llm"
)

func newClient(url string, retries int) (*llm.Groq, *[]time.Duration) {
	g := llm.NewGroq("test-key", url, "m", 100, 100, retries)
	var sleeps []time.Duration
	g.Sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }
	return g, &sleeps
}

func reply(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
}

func TestTextSendsAuthAndParsesReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("bad request %s auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		reply(w, "  hello  ")
	}))
	defer srv.Close()
	g, _ := newClient(srv.URL, 0)
	got, err := g.Text(context.Background(), "s", "u")
	if err != nil || got != "hello" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestJSONSendsStrictSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rf := body["response_format"].(map[string]any)
		js := rf["json_schema"].(map[string]any)
		if rf["type"] != "json_schema" || js["strict"] != true || js["name"] != "grades" {
			t.Errorf("bad response_format: %v", rf)
		}
		reply(w, `{"ok":true}`)
	}))
	defer srv.Close()
	g, _ := newClient(srv.URL, 0)
	got, err := g.JSON(context.Background(), "s", "u", "grades", map[string]any{"type": "object"})
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("got %s %v", got, err)
	}
}

func TestEmptyJSONReplyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reply(w, "") }))
	defer srv.Close()
	g, _ := newClient(srv.URL, 0)
	if _, err := g.JSON(context.Background(), "s", "u", "n", map[string]any{}); err == nil {
		t.Fatal("expected error for empty reply")
	}
}

func TestRetriesRateLimitHonouringRetryAfter(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		reply(w, "ok")
	}))
	defer srv.Close()
	g, sleeps := newClient(srv.URL, 5)
	got, err := g.Text(context.Background(), "s", "u")
	if err != nil || got != "ok" || n.Load() != 3 {
		t.Fatalf("got %q %v after %d calls", got, err, n.Load())
	}
	if len(*sleeps) != 2 || (*sleeps)[0] != 2*time.Second {
		t.Fatalf("sleeps %v", *sleeps)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	g, _ := newClient(srv.URL, 5)
	_, err := g.Text(context.Background(), "s", "u")
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || n.Load() != 1 {
		t.Fatalf("err %v calls %d", err, n.Load())
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	g, _ := newClient(srv.URL, 2)
	if _, err := g.Text(context.Background(), "s", "u"); err == nil || n.Load() != 3 {
		t.Fatalf("err %v calls %d (want 3)", err, n.Load())
	}
}

func TestContextCancelStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	g := llm.NewGroq("k", srv.URL, "m", 1, 1, 5)
	ctx, cancel := context.WithCancel(context.Background())
	g.Sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	if _, err := g.Text(ctx, "s", "u"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}
