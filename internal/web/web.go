// Package web is the fallback: rewrite the question into a search query, then search DuckDuckGo.
package web

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"crag/internal/llm"
)

// Result is one web search hit.
type Result struct {
	Title, Snippet, URL string
}

// Searcher runs a web search. Tests replace it with a fake.
type Searcher interface {
	Search(ctx context.Context, query string, max int) ([]Result, error)
}

// RewriteQuery turns a chatty question into keywords a search engine handles well.
func RewriteQuery(ctx context.Context, c llm.Client, question, helpDesk, hint string) (string, error) {
	system := fmt.Sprintf(`You work at an %s help desk. Rewrite the user's question as a
short web search query of 4 to 10 keywords. Keep names, places and numbers.
%s
Reply with the query only, no quotes.`, helpDesk, hint)
	q, err := c.Text(ctx, system, question)
	if err != nil {
		return question, fmt.Errorf("rewrite query: %w", err)
	}
	if q = strings.Trim(strings.TrimSpace(q), `"'`); q == "" {
		return question, nil
	}
	return q, nil
}

// Format renders results as "<title>: <snippet> [WEB:<url>]" lines for the answer step.
func Format(results []Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, fmt.Sprintf("%s: %s [WEB:%s]", r.Title, r.Snippet, r.URL))
	}
	return out
}

// DuckDuckGo searches the keyless HTML endpoint.
type DuckDuckGo struct {
	Endpoint string // default https://html.duckduckgo.com/html/
	HTTP     *http.Client
}

// NewDuckDuckGo returns a searcher with a 15 second timeout.
func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{
		Endpoint: "https://html.duckduckgo.com/html/",
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Search implements Searcher. It retries twice when DuckDuckGo throttles us.
func (d *DuckDuckGo) Search(ctx context.Context, query string, max int) ([]Result, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		body, status, err := d.fetch(ctx, query)
		if err != nil {
			lastErr = err
			continue
		}
		if status == http.StatusTooManyRequests || status == http.StatusAccepted || status >= 500 {
			lastErr = fmt.Errorf("duckduckgo status %d", status)
			continue
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("duckduckgo status %d", status)
		}
		return ParseResults(body, max), nil
	}
	return nil, lastErr
}

func (d *DuckDuckGo) fetch(ctx context.Context, query string) (string, int, error) {
	form := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; corrective-rag/1.0)")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(raw), resp.StatusCode, err
}

var (
	titleRe   = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRe = regexp.MustCompile(`(?s)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// ParseResults extracts hits from DuckDuckGo's HTML page, skipping ads.
func ParseResults(page string, max int) []Result {
	titles := titleRe.FindAllStringSubmatch(page, -1)
	snippets := snippetRe.FindAllStringSubmatch(page, -1)
	var out []Result
	for i, t := range titles {
		if len(out) >= max {
			break
		}
		link := resolveLink(html.UnescapeString(t[1]))
		if link == "" || strings.Contains(link, "duckduckgo.com/y.js") {
			continue // ad or unusable redirect
		}
		r := Result{Title: clean(t[2]), URL: link}
		if i < len(snippets) {
			r.Snippet = clean(snippets[i][1])
		}
		if r.Title != "" {
			out = append(out, r)
		}
	}
	return out
}

// resolveLink unwraps DuckDuckGo's //duckduckgo.com/l/?uddg=<url> redirects.
func resolveLink(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if target := u.Query().Get("uddg"); target != "" {
		return target
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return href
	}
	return ""
}

func clean(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(s, "")), " "))
}

// Fake is a Searcher for tests.
type Fake struct {
	Results []Result
	Err     error
	Queries []string
}

// Search implements Searcher.
func (f *Fake) Search(_ context.Context, query string, _ int) ([]Result, error) {
	f.Queries = append(f.Queries, query)
	return f.Results, f.Err
}
