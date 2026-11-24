package server_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"crag/internal/pipeline"
	"crag/internal/server"
)

type fakeStreamer struct {
	events []pipeline.Event
	res    *pipeline.Result
	err    error
	delay  time.Duration
}

func (f *fakeStreamer) Ask(ctx context.Context, q string) (*pipeline.Result, error) {
	return f.AskStream(ctx, q, nil)
}

func (f *fakeStreamer) AskStream(ctx context.Context, _ string, emit func(pipeline.Event)) (*pipeline.Result, error) {
	for _, e := range f.events {
		if emit != nil {
			emit(e)
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.res, f.err
}

// eventNames lists the "event:" lines of an SSE body in order.
func eventNames(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestStreamSendsProgressThenResult(t *testing.T) {
	a := &fakeStreamer{
		events: []pipeline.Event{{Type: pipeline.EventRetrieved}, {Type: pipeline.EventGraded, Verdict: "CORRECT", Route: []string{"kb"}}},
		res:    &pipeline.Result{Answer: "Yes. [KB:tdr]", Verdict: "CORRECT"},
	}
	rec := do(handler(a, 1), "POST", "/ask/stream", `{"question":"how do I file a TDR?"}`)

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if got := strings.Join(eventNames(rec.Body.String()), ","); got != "progress,progress,result" {
		t.Fatalf("events %q in\n%s", got, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `data: {"type":"graded","ms":0,"verdict":"CORRECT","route":["kb"]}`) {
		t.Fatalf("progress payload: %s", rec.Body)
	}
	if !rec.Flushed {
		t.Fatal("the stream was never flushed, so a browser would see nothing until the end")
	}
}

func TestStreamFallsBackToAskWithoutProgress(t *testing.T) {
	a := &fakeAsker{res: &pipeline.Result{Answer: "Yes.", Verdict: "CORRECT"}}
	rec := do(handler(a, 1), "POST", "/ask/stream", `{"question":"how do I file a TDR?"}`)
	if got := strings.Join(eventNames(rec.Body.String()), ","); got != "result" {
		t.Fatalf("events %q", got)
	}
}

func TestStreamRejectsBeforeStartingTheStream(t *testing.T) {
	h := handler(&fakeStreamer{res: &pipeline.Result{}}, 1)
	for body, want := range map[string]int{
		`{`:                 400,
		`{"question":"hi"}`: 422,
		`{"question":"` + strings.Repeat("a", 51) + `"}`: 422,
	} {
		rec := do(h, "POST", "/ask/stream", body)
		if rec.Code != want || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: got %d %q", body, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := do(h, "GET", "/ask/stream", ""); rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
}

func TestStreamBusyReturns429AsJSON(t *testing.T) {
	a := &fakeAsker{res: &pipeline.Result{}, gate: make(chan struct{})}
	h := handler(a, 1)
	done := make(chan struct{})
	go func() { do(h, "POST", "/ask/stream", `{"question":"first question"}`); close(done) }()
	time.Sleep(100 * time.Millisecond)

	rec := do(h, "POST", "/ask/stream", `{"question":"second question"}`)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("got %d %v", rec.Code, rec.Header())
	}
	close(a.gate)
	<-done
}

func TestStreamErrorsDoNotLeakDetails(t *testing.T) {
	rec := do(handler(&fakeStreamer{err: errors.New("secret groq key gsk_123")}, 1), "POST", "/ask/stream", `{"question":"hello there"}`)
	body := rec.Body.String()
	if got := strings.Join(eventNames(body), ","); got != "error" || strings.Contains(body, "gsk_123") {
		t.Fatalf("events %q body %s", got, body)
	}

	rec = do(handler(&fakeStreamer{err: context.DeadlineExceeded}, 1), "POST", "/ask/stream", `{"question":"hello there"}`)
	if !strings.Contains(rec.Body.String(), "took too long") {
		t.Fatalf("timeout body: %s", rec.Body)
	}
}

func TestStreamKeepsAliveWhileTheModelIsSlow(t *testing.T) {
	defer server.SetHeartbeat(10 * time.Millisecond)()
	a := &fakeStreamer{res: &pipeline.Result{Answer: "Yes."}, delay: 120 * time.Millisecond}
	rec := do(handler(a, 1), "POST", "/ask/stream", `{"question":"how do I file a TDR?"}`)

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("no heartbeat in %q", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "}") || !strings.Contains(body, "event: result") {
		t.Fatalf("the result must still arrive after the heartbeats: %q", body)
	}
}
