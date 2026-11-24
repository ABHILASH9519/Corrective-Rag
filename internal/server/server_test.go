package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crag/internal/pipeline"
	"crag/internal/server"
)

type fakeAsker struct {
	res  *pipeline.Result
	err  error
	gate chan struct{} // if set, Ask blocks until closed
}

func (f *fakeAsker) Ask(ctx context.Context, q string) (*pipeline.Result, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.res, f.err
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func handler(a server.Asker, concurrent int) http.Handler {
	return server.New(a, "model-x", "9.9.9", 50, concurrent).Handler()
}

func TestHealth(t *testing.T) {
	rec := do(handler(&fakeAsker{}, 1), "GET", "/health", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"version":"9.9.9"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestAskOK(t *testing.T) {
	a := &fakeAsker{res: &pipeline.Result{Answer: "Yes. [KB:tdr]", Verdict: "CORRECT", Sources: []string{"KB:tdr"}}}
	rec := do(handler(a, 1), "POST", "/ask", `{"question":"how do I file a TDR?"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"verdict":"CORRECT"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestAskValidation(t *testing.T) {
	h := handler(&fakeAsker{res: &pipeline.Result{}}, 1)
	cases := map[string]struct {
		body string
		want int
	}{
		"bad json":      {`{`, 400},
		"unknown field": {`{"question":"hello there","x":1}`, 400},
		"too short":     {`{"question":"hi"}`, 422},
		"too long":      {`{"question":"` + strings.Repeat("a", 51) + `"}`, 422},
	}
	for name, c := range cases {
		if rec := do(h, "POST", "/ask", c.body); rec.Code != c.want {
			t.Errorf("%s: got %d want %d", name, rec.Code, c.want)
		}
	}
	if rec := do(h, "GET", "/ask", ""); rec.Code != 405 {
		t.Errorf("GET /ask: %d", rec.Code)
	}
}

func TestErrorsDoNotLeakDetails(t *testing.T) {
	rec := do(handler(&fakeAsker{err: errors.New("secret groq key gsk_123")}, 1), "POST", "/ask", `{"question":"hello there"}`)
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "gsk_123") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(handler(&fakeAsker{err: context.DeadlineExceeded}, 1), "POST", "/ask", `{"question":"hello there"}`)
	if rec.Code != 504 {
		t.Fatalf("timeout status %d", rec.Code)
	}
}

func TestBusyReturns429(t *testing.T) {
	a := &fakeAsker{res: &pipeline.Result{}, gate: make(chan struct{})}
	h := handler(a, 1)
	done := make(chan int)
	go func() { done <- do(h, "POST", "/ask", `{"question":"first question"}`).Code }()
	time.Sleep(100 * time.Millisecond) // let the first request take the only slot

	rec := do(h, "POST", "/ask", `{"question":"second question"}`)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d", rec.Code)
	}
	close(a.gate)
	if code := <-done; code != 200 {
		t.Fatalf("first request got %d", code)
	}
}
