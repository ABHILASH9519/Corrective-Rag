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
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, errors.New("question is empty")
	}
	docs := p.index.Retrieve(question, p.opts.TopK)

	labels, err := evaluator.Grade(ctx, p.llm, question, docs)
	if err != nil {
		return nil, err
	}
	res := &Result{Question: question, Verdict: evaluator.Decide(labels)}
	for _, d := range docs {
		res.Passages = append(res.Passages, Passage{d.ID, d.Similarity, labels[d.ID]})
	}
	slog.Info("graded", "verdict", res.Verdict, "passages", len(docs))

	doRefine := res.Verdict == evaluator.Correct || res.Verdict == evaluator.Ambiguous
	doWeb := res.Verdict == evaluator.Incorrect || res.Verdict == evaluator.Ambiguous

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
		}()
	}
	if doWeb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			webQuery, webKnowledge = p.webFallback(ctx, question)
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

// webFallback never fails the request: a broken search just means less knowledge.
func (p *Pipeline) webFallback(ctx context.Context, question string) (string, []string) {
	query, err := web.RewriteQuery(ctx, p.llm, question, p.opts.HelpDesk, p.opts.Hint)
	if err != nil {
		slog.Warn("query rewrite failed, using the question as the query", "err", err)
		query = question
	}
	results, err := p.searcher.Search(ctx, query, p.opts.WebResults)
	if err != nil {
		slog.Warn("web search failed", "err", err)
		return query, nil
	}
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
