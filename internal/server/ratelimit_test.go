package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crag/internal/pipeline"
)

func TestLimiterBurstThenSteadyRate(t *testing.T) {
	l := newLimiter(10)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < 5; i++ { // burst is capped at 5
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("request %d should pass within burst", i+1)
		}
	}
	ok, wait := l.allow("a")
	if ok || wait <= 0 || wait > 7*time.Second {
		t.Fatalf("6th request: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("a different client has its own bucket")
	}
	now = now.Add(7 * time.Second) // 10/min refills one token in 6s
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("token should have refilled")
	}
}

func TestLimiterForgetsIdleClients(t *testing.T) {
	l := newLimiter(10)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	l.allow("old")
	now = now.Add(30 * time.Minute)
	l.allow("new")
	if _, ok := l.buckets["old"]; ok {
		t.Fatal("idle bucket should be collected")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := clientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted proxy: got %s", got)
	}
	if got := clientIP(r, true); got != "203.0.113.9" { // rightmost = added by our proxy, not spoofable
		t.Errorf("trusted proxy: got %s", got)
	}
}

type okAsker struct{}

func (okAsker) Ask(_ context.Context, _ string) (*pipeline.Result, error) {
	return &pipeline.Result{}, nil
}

func TestAskIsRateLimitedPerClient(t *testing.T) {
	h := New(okAsker{}, "m", "v", 100, 4, RateLimit{PerMinute: 2}).Handler()
	post := func(ip string) int {
		req := httptest.NewRequest("POST", "/ask", strings.NewReader(`{"question":"hello there"}`))
		req.RemoteAddr = ip + ":1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if post("1.1.1.1") != 200 || post("1.1.1.1") != http.StatusOK {
		t.Fatal("first two requests should pass (burst = rate)")
	}
	if code := post("1.1.1.1"); code != http.StatusTooManyRequests {
		t.Fatalf("third request got %d", code)
	}
	if code := post("2.2.2.2"); code != http.StatusOK {
		t.Fatalf("other client got %d", code)
	}
}

func TestIndexServesUIWithSecurityHeaders(t *testing.T) {
	h := New(okAsker{}, "m", "v", 100, 4, RateLimit{}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "RAGForge") || !strings.Contains(rec.Body.String(), "/ask/stream") {
		t.Fatalf("%d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") ||
		!strings.Contains(csp, "connect-src 'self'") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown path got %d", rec.Code)
	}
}
