package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"crag/internal/llmtest"
	"crag/internal/web"
)

const page = `
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fbaggage&amp;rut=abc">IndiGo <b>baggage</b> policy</a>
<a class="result__snippet" href="x">Domestic free check-in is <b>15 kg</b> &amp; 7 kg cabin.</a></div>
<div class="result"><a class="result__a" href="https://duckduckgo.com/y.js?ad=1">Sponsored</a>
<a class="result__snippet" href="x">Buy now</a></div>
<div class="result"><a class="result__a" href="https://example.org/two">Second</a>
<a class="result__snippet" href="x">Another result.</a></div>`

func TestParseResults(t *testing.T) {
	got := web.ParseResults(page, 5)
	if len(got) != 2 {
		t.Fatalf("want 2 (ad skipped), got %d: %+v", len(got), got)
	}
	if got[0].URL != "https://example.com/baggage" || got[0].Title != "IndiGo baggage policy" ||
		got[0].Snippet != "Domestic free check-in is 15 kg & 7 kg cabin." {
		t.Fatalf("bad first result: %+v", got[0])
	}
	if got[1].URL != "https://example.org/two" {
		t.Fatalf("bad second: %+v", got[1])
	}
	if n := len(web.ParseResults(page, 1)); n != 1 {
		t.Fatalf("max not honoured: %d", n)
	}
}

func TestFormat(t *testing.T) {
	got := web.Format([]web.Result{{Title: "T", Snippet: "S", URL: "https://u"}})
	if len(got) != 1 || got[0] != "T: S [WEB:https://u]" {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteQuery(t *testing.T) {
	f := &llmtest.Fake{TextReply: `  "IndiGo baggage allowance"  `}
	q, err := web.RewriteQuery(context.Background(), f, "chatty question", "Indian Railways", "")
	if err != nil || q != "IndiGo baggage allowance" {
		t.Fatalf("got %q %v", q, err)
	}
	empty := &llmtest.Fake{TextReply: " "}
	if q, _ := web.RewriteQuery(context.Background(), empty, "orig", "x", ""); q != "orig" {
		t.Fatalf("empty rewrite should fall back, got %q", q)
	}
}

func TestDuckDuckGoSearch(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("q") != "indigo baggage" {
			t.Errorf("bad request form: %v %v", err, r.PostForm)
		}
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	d := &web.DuckDuckGo{Endpoint: srv.URL, HTTP: srv.Client()}
	got, err := d.Search(context.Background(), "indigo baggage", 5)
	if err != nil || len(got) != 2 || hits.Load() != 1 {
		t.Fatalf("got %v err %v hits %d", got, err, hits.Load())
	}
}

func TestDuckDuckGoClientErrorIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	d := &web.DuckDuckGo{Endpoint: srv.URL, HTTP: srv.Client()}
	if _, err := d.Search(context.Background(), "q", 3); err == nil || hits.Load() != 1 {
		t.Fatalf("err %v hits %d", err, hits.Load())
	}
}
