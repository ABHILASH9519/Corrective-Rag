// Package server exposes the pipeline over HTTP: POST /ask, POST /ask/stream and GET /health.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"crag/internal/pipeline"
)

//go:embed ui/index.html
var indexHTML []byte

// heartbeat keeps proxies from closing a stream while the model is slow or retrying.
// It is a variable only so a test can shorten it.
var heartbeat = 10 * time.Second

// Asker is what the server needs from the pipeline.
type Asker interface {
	Ask(ctx context.Context, question string) (*pipeline.Result, error)
}

// Streamer is an Asker that can also report progress. The pipeline implements
// it; /ask/stream falls back to Ask, with no progress events, for an Asker
// that does not.
type Streamer interface {
	Asker
	AskStream(ctx context.Context, question string, emit func(pipeline.Event)) (*pipeline.Result, error)
}

// Server holds the HTTP handlers.
type Server struct {
	asker       Asker
	model       string
	version     string
	maxQuestion int
	slots       chan struct{} // limits concurrent pipeline runs
	timeout     time.Duration
	limiter     *limiter // nil = unlimited
	trustProxy  bool
}

// New builds a Server. maxConcurrent caps simultaneous questions so one burst
// cannot exhaust the Groq rate limit; extra callers get 429.
func New(a Asker, model, version string, maxQuestion, maxConcurrent int, rl RateLimit) *Server {
	s := &Server{
		asker: a, model: model, version: version, maxQuestion: maxQuestion,
		slots: make(chan struct{}, maxConcurrent), timeout: 90 * time.Second,
		trustProxy: rl.TrustProxy,
	}
	if rl.PerMinute > 0 {
		s.limiter = newLimiter(rl.PerMinute)
	}
	return s
}

// Handler returns the routes wrapped with request logging.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /ask", s.ask)
	mux.HandleFunc("POST /ask/stream", s.askStream)
	return logRequests(mux)
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(indexHTML)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version, "model": s.model})
}

type askRequest struct {
	Question string `json:"question"`
}

// admit validates the request and reserves a slot. When ok is false the
// response has already been written. Otherwise the caller must call release.
func (s *Server) admit(w http.ResponseWriter, r *http.Request) (question string, release func(), ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req askRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON like {\"question\": \"...\"}")
		return "", nil, false
	}
	q := strings.TrimSpace(req.Question)
	switch {
	case len(q) < 3:
		writeError(w, http.StatusUnprocessableEntity, "question must be at least 3 characters")
		return "", nil, false
	case len(q) > s.maxQuestion:
		writeError(w, http.StatusUnprocessableEntity, "question is too long")
		return "", nil, false
	}

	if s.limiter != nil {
		if allowed, wait := s.limiter.allow(clientIP(r, s.trustProxy)); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "you are asking too quickly, please wait a moment")
			return "", nil, false
		}
	}

	select {
	case s.slots <- struct{}{}:
		return q, func() { <-s.slots }, true
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "the help desk is busy, try again shortly")
		return "", nil, false
	}
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	q, release, ok := s.admit(w, r)
	if !ok {
		return
	}
	defer release()

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	res, err := s.asker.Ask(ctx, q)
	if err != nil {
		slog.Error("pipeline failed", "err", err)
		writeError(w, failureStatus(err), "the help desk could not answer right now") // never leak provider details
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// askStream answers like ask but sends server-sent events as the pipeline
// works: "progress" for each step, then one "result" or one "error".
// Problems found before the stream starts (bad body, rate limit, busy) are
// plain JSON errors with the usual status, exactly as /ask returns them.
func (s *Server) askStream(w http.ResponseWriter, r *http.Request) {
	q, release, ok := s.admit(w, r)
	if !ok {
		return
	}
	defer release()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // ask nginx-style proxies not to buffer the stream
	w.WriteHeader(http.StatusOK)
	stream := &sseWriter{w: w, rc: http.NewResponseController(w)}
	stream.flush()

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()

	stopBeat := stream.startHeartbeat(ctx)
	var (
		res *pipeline.Result
		err error
	)
	if st, ok := s.asker.(Streamer); ok {
		res, err = st.AskStream(ctx, q, func(e pipeline.Event) { stream.send("progress", e) })
	} else {
		res, err = s.asker.Ask(ctx, q)
	}
	stopBeat() // no write may happen after the handler returns

	if err != nil {
		slog.Error("pipeline failed", "err", err)
		msg := "the help desk could not answer right now" // never leak provider details
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "the help desk took too long to answer"
		}
		stream.send("error", map[string]string{"error": msg})
		return
	}
	stream.send("result", res)
}

func failureStatus(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

// sseWriter writes server-sent events. Writes are serialised because the
// heartbeat and the pipeline's progress callback run on different goroutines.
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (s *sseWriter) flush() { _ = s.rc.Flush() }

func (s *sseWriter) send(event string, v any) {
	data, err := json.Marshal(v) // never contains a raw newline, so one data line is enough
	if err != nil {
		slog.Error("encode event", "event", event, "err", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	s.flush()
}

// startHeartbeat sends an SSE comment every few seconds until the returned
// stop function is called, and does not return until the goroutine has exited.
func (s *sseWriter) startHeartbeat(ctx context.Context) (stop func()) {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				s.mu.Lock()
				_, _ = fmt.Fprint(s.w, ": keep-alive\n\n")
				s.flush()
				s.mu.Unlock()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-exited
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Unwrap lets http.ResponseController reach the real writer to flush it.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}
