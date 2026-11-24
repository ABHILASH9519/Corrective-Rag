// Package eval measures the help desk against a labelled question set.
//
// Two checks, so quality is a number rather than a feeling:
//   - Retrieval (offline, free): is the right passage in the top-k?
//   - Routing (live, needs Groq): does the evaluator choose the right CRAG action?
//     In-KB questions should be CORRECT; out-of-KB questions should be INCORRECT.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"crag/internal/kb"
	"crag/internal/pipeline"
)

// Case is one labelled question. Source is the passage that answers it; it is
// empty for questions the knowledge base cannot answer.
type Case struct {
	Question string `json:"question"`
	Expect   string `json:"expect"` // CORRECT or INCORRECT
	Source   string `json:"source,omitempty"`
}

// Load parses an evaluation set.
func Load(raw []byte) ([]Case, error) {
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		return nil, fmt.Errorf("evaluation set: %w", err)
	}
	for i, c := range cases {
		if strings.TrimSpace(c.Question) == "" || (c.Expect != "CORRECT" && c.Expect != "INCORRECT") {
			return nil, fmt.Errorf("evaluation case #%d needs a question and expect CORRECT or INCORRECT", i)
		}
		if c.Expect == "CORRECT" && c.Source == "" {
			return nil, fmt.Errorf("evaluation case #%d expects CORRECT but names no source passage", i)
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("evaluation set is empty")
	}
	return cases, nil
}

// RetrievalReport is the offline result.
type RetrievalReport struct {
	Answerable int
	Top1, TopK int
	K          int
	Misses     []string
}

// Retrieval checks, for every answerable question, whether the source passage
// is ranked first and whether it is within the top k.
func Retrieval(index *kb.Index, cases []Case, k int) RetrievalReport {
	r := RetrievalReport{K: k}
	for _, c := range cases {
		if c.Expect != "CORRECT" {
			continue
		}
		r.Answerable++
		docs := index.Retrieve(c.Question, k)
		if len(docs) > 0 && docs[0].ID == c.Source {
			r.Top1++
		}
		hit := false
		for _, d := range docs {
			if d.ID == c.Source {
				hit = true
			}
		}
		if hit {
			r.TopK++
		} else {
			r.Misses = append(r.Misses, c.Question)
		}
	}
	return r
}

// Write prints the report.
func (r RetrievalReport) Write(w io.Writer) {
	fmt.Fprintf(w, "Retrieval (offline, %d answerable questions)\n", r.Answerable)
	fmt.Fprintf(w, "  top-1 hit rate : %s\n", pct(r.Top1, r.Answerable))
	fmt.Fprintf(w, "  top-%d hit rate : %s\n", r.K, pct(r.TopK, r.Answerable))
	for _, m := range r.Misses {
		fmt.Fprintf(w, "  miss: %s\n", m)
	}
}

// RoutingReport is the live result.
type RoutingReport struct {
	Total, Right      int
	InKB, InKBRight   int
	OutKB, OutKBRight int
	Wrong             []string
}

// Routing runs every question through the full pipeline and compares the
// verdict with the label. AMBIGUOUS is acceptable for in-KB questions
// (the pipeline still consults the KB) but not for out-of-KB ones.
func Routing(ctx context.Context, p *pipeline.Pipeline, cases []Case, progress io.Writer) (RoutingReport, error) {
	var r RoutingReport
	for i, c := range cases {
		res, err := p.Ask(ctx, c.Question)
		if err != nil {
			return r, fmt.Errorf("case %d (%q): %w", i+1, c.Question, err)
		}
		ok := Acceptable(c.Expect, res.Verdict)
		r.Total++
		if c.Expect == "CORRECT" {
			r.InKB++
			if ok {
				r.InKBRight++
			}
		} else {
			r.OutKB++
			if ok {
				r.OutKBRight++
			}
		}
		if ok {
			r.Right++
		} else {
			r.Wrong = append(r.Wrong, fmt.Sprintf("%s -> got %s, want %s", c.Question, res.Verdict, c.Expect))
		}
		if progress != nil {
			mark := "ok  "
			if !ok {
				mark = "FAIL"
			}
			fmt.Fprintf(progress, "[%2d/%d] %s %-9s %s\n", i+1, len(cases), mark, res.Verdict, c.Question)
		}
	}
	return r, nil
}

// Acceptable reports whether a verdict is a correct routing for the label.
func Acceptable(expect, verdict string) bool {
	if expect == "CORRECT" {
		return verdict == "CORRECT" || verdict == "AMBIGUOUS"
	}
	return verdict == "INCORRECT"
}

// Write prints the report.
func (r RoutingReport) Write(w io.Writer) {
	fmt.Fprintf(w, "\nRouting (live)\n")
	fmt.Fprintf(w, "  overall           : %s\n", pct(r.Right, r.Total))
	fmt.Fprintf(w, "  in-KB -> KB used  : %s\n", pct(r.InKBRight, r.InKB))
	fmt.Fprintf(w, "  out-of-KB -> web  : %s\n", pct(r.OutKBRight, r.OutKB))
	for _, m := range r.Wrong {
		fmt.Fprintf(w, "  wrong: %s\n", m)
	}
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", n, d, 100*float64(n)/float64(d))
}
