// Package server exposes the pipeline over HTTP: POST /ask and GET /health.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crag/internal/pipeline"
)

//go:embed ui/index.html
var indexHTML []byte

// Asker is what the server needs from the pipeline.
type Asker interface {
	Ask(ctx context.Context, question string) (*pipeline.Result, error)
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
	return logRequests(mux)
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(indexHTML)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version, "model": s.model})
}

type askRequest struct {
	Question string `json:"question"`
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req askRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON like {\"question\": \"...\"}")
		return
	}
	q := strings.TrimSpace(req.Question)
	switch {
	case len(q) < 3:
		writeError(w, http.StatusUnprocessableEntity, "question must be at least 3 characters")
		return
	case len(q) > s.maxQuestion:
		writeError(w, http.StatusUnprocessableEntity, "question is too long")
		return
	}

	if s.limiter != nil {
		if ok, wait := s.limiter.allow(clientIP(r, s.trustProxy)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "you are asking too quickly, please wait a moment")
			return
		}
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "the help desk is busy, try again shortly")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	res, err := s.asker.Ask(ctx, q)
	if err != nil {
		slog.Error("pipeline failed", "err", err)
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, "the help desk could not answer right now") // never leak provider details
		return
	}
	writeJSON(w, http.StatusOK, res)
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

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}
