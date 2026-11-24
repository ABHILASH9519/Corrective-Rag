// Package pipeline wires the CRAG flow: retrieve, grade, then refine the KB,
// search the web, or both at once, and finally write a cited answer.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"crag/internal/evaluator"
	"crag/internal/kb"
	"crag/internal/llm"
	"crag/internal/refine"
	"crag/internal/web"
)

// NoAnswer is returned when neither the KB nor the web has anything useful.
const NoAnswer = "I don't know - I couldn't find reliable information to answer that."

const answerSystem = `You are a %s help desk assistant.
Answer only from the knowledge given. Each line ends with its source tag, such as [KB:tdr] or [WEB:https://...].
Cite the tags you used at the end of the sentence that uses them, copied exactly in square brackets.
If the knowledge does not answer the question, say you don't know. Keep it under 100 words.`

// Passage is a retrieved passage with its grade.
type Passage struct {
	ID         string  `json:"id"`
	Similarity float64 `json:"similarity"`
	Label      string  `json:"label"`
	Text       string  `json:"text,omitempty"`
}

// Result is everything the pipeline learned about one question.
type Result struct {
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Verdict   string    `json:"verdict"`
	Sources   []string  `json:"sources"`
	Passages  []Passage `json:"passages"`
	WebQuery  string    `json:"web_query,omitempty"`
	Knowledge []string  `json:"-"`
}

// Progress event types, in the order a question normally moves through them.
// Refine and web events can interleave because the two branches run at once.
const (
	EventRetrieved  = "retrieved"  // passages found, not graded yet
	EventGraded     = "graded"     // passages labelled, verdict and route decided
	EventRewritten  = "rewritten"  // web query written
	EventSearched   = "searched"   // web search finished; Count is the snippets found
	EventRefined    = "refined"    // notes filtered; Count is the sentences kept
	EventGenerating = "generating" // writing the final answer
)

// Routes a verdict can take. A verdict can take both.
const (
	RouteKB  = "kb"
	RouteWeb = "web"
)

// Event reports one step of a question in progress, so a UI can show the
// real work instead of a spinner. MS is the time since the question started.
type Event struct {
	Type     string    `json:"type"`
	MS       int64     `json:"ms"`
	Passages []Passage `json:"passages,omitempty"`
	Verdict  string    `json:"verdict,omitempty"`
	Route    []string  `json:"route,omitempty"`
	WebQuery string    `json:"web_query,omitempty"`
	Count    int       `json:"count,omitempty"`
}

// Options configures a Pipeline.
type Options struct {
	TopK       int
	WebResults int
	HelpDesk   string // e.g. "Indian Railways"
	Hint       string // extra rule for the query rewriter
}

// Pipeline answers questions. It is safe for concurrent use.
type Pipeline struct {
	index    *kb.Index
	llm      llm.Client
	searcher web.Searcher
	opts     Options
}

// New builds a Pipeline.
func New(index *kb.Index, c llm.Client, s web.Searcher, opts Options) *Pipeline {
	if opts.TopK < 1 {
		opts.TopK = 3
	}
	if opts.WebResults < 1 {
		opts.WebResults = 4
	}
	return &Pipeline{index: index, llm: c, searcher: s, opts: opts}
}

// Ask runs one question through the whole pipeline.
func (p *Pipeline) Ask(ctx context.Context, question string) (*Result, error) {
	return p.AskStream(ctx, question, nil)
}

// AskStream is Ask, and also calls emit as each step finishes. emit is never
// called from two goroutines at once and is not called after AskStream
// returns. It may be nil.
func (p *Pipeline) AskStream(ctx context.Context, question string, emit func(Event)) (*Result, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, errors.New("question is empty")
	}

	start := time.Now()
	var emitMu sync.Mutex
	send := func(e Event) {
		if emit == nil {
			return
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		e.MS = time.Since(start).Milliseconds()
		emit(e)
	}

	docs := p.index.Retrieve(question, p.opts.TopK)
	send(Event{Type: EventRetrieved, Passages: passagesOf(docs, nil)})

	labels, err := evaluator.Grade(ctx, p.llm, question, docs)
	if err != nil {
		return nil, err
	}
	res := &Result{Question: question, Verdict: evaluator.Decide(labels), Passages: passagesOf(docs, labels)}
	slog.Info("graded", "verdict", res.Verdict, "passages", len(docs))

	doRefine := res.Verdict == evaluator.Correct || res.Verdict == evaluator.Ambiguous
	doWeb := res.Verdict == evaluator.Incorrect || res.Verdict == evaluator.Ambiguous
	var route []string
	if doRefine {
		route = append(route, RouteKB)
	}
	if doWeb {
		route = append(route, RouteWeb)
	}
	send(Event{Type: EventGraded, Passages: res.Passages, Verdict: res.Verdict, Route: route})

	var (
		wg                    sync.WaitGroup
		refined, webKnowledge []string
		refineErr             error
		webQuery              string
	)
	if doRefine {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refined, refineErr = refine.Refine(ctx, p.llm, question, docs)
			if refineErr == nil {
				send(Event{Type: EventRefined, Count: len(refined)})
			}
		}()
	}
	if doWeb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			webQuery, webKnowledge = p.webFallback(ctx, question, send)
		}()
	}
	wg.Wait()
	if refineErr != nil {
		return nil, refineErr
	}

	res.WebQuery = webQuery
	// KB knowledge first so the answer prefers curated notes over web snippets.
	res.Knowledge = append(refined, webKnowledge...)
	if len(res.Knowledge) == 0 {
		res.Answer = NoAnswer
		return res, nil
	}
	send(Event{Type: EventGenerating})
	user := fmt.Sprintf("Question: %s\n\nKnowledge:\n%s", question, strings.Join(res.Knowledge, "\n"))
	answer, err := p.llm.Text(ctx, fmt.Sprintf(answerSystem, p.opts.HelpDesk), user)
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
	}
	if answer = strings.TrimSpace(answer); answer == "" {
		answer = NoAnswer
	}
	res.Answer = answer
	res.Sources = Sources(answer)
	return res, nil
}

// passagesOf pairs retrieved docs with their labels; labels may be nil before grading.
func passagesOf(docs []kb.Doc, labels map[string]string) []Passage {
	out := make([]Passage, 0, len(docs))
	for _, d := range docs {
		out = append(out, Passage{ID: d.ID, Similarity: d.Similarity, Label: labels[d.ID], Text: d.Text})
	}
	return out
}

// webFallback never fails the request: a broken search just means less knowledge.
func (p *Pipeline) webFallback(ctx context.Context, question string, send func(Event)) (string, []string) {
	query, err := web.RewriteQuery(ctx, p.llm, question, p.opts.HelpDesk, p.opts.Hint)
	if err != nil {
		slog.Warn("query rewrite failed, using the question as the query", "err", err)
		query = question
	}
	send(Event{Type: EventRewritten, WebQuery: query})
	results, err := p.searcher.Search(ctx, query, p.opts.WebResults)
	if err != nil {
		slog.Warn("web search failed", "err", err)
		send(Event{Type: EventSearched})
		return query, nil
	}
	send(Event{Type: EventSearched, Count: len(results)})
	return query, web.Format(results)
}

var tagRe = regexp.MustCompile(`\[((?:KB|WEB):[^\]]+)\]`)

// Sources returns the distinct source tags (KB:id / WEB:url) the answer cites, in order.
func Sources(answer string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tagRe.FindAllStringSubmatch(answer, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
