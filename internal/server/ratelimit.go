package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimit configures per-client throttling of POST /ask.
type RateLimit struct {
	PerMinute  int  // 0 disables limiting
	TrustProxy bool // take the client IP from X-Forwarded-For (only behind a trusted proxy)
}

// limiter is a token bucket per client, refilled continuously.
type limiter struct {
	mu      sync.Mutex
	perMin  float64
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(perMinute int) *limiter {
	burst := float64(perMinute)
	if burst > 5 {
		burst = 5 // allow a short burst, then the steady rate
	}
	return &limiter{perMin: float64(perMinute), burst: burst, buckets: map[string]*bucket{}, now: time.Now}
}

// allow takes a token for key. When denied it returns how long until one is free.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Minutes() * l.perMin
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.perMin * float64(time.Minute))
	return false, wait
}

// gc drops idle buckets so the map cannot grow without bound.
func (l *limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// clientIP identifies the caller. Behind a trusted proxy the rightmost
// X-Forwarded-For entry is the one the proxy itself appended, so it cannot be
// spoofed by the client the way the leftmost can.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
