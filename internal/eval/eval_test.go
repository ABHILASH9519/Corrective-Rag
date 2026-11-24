package eval_test

import (
	"testing"

	"crag/data"
	"crag/internal/eval"
	"crag/internal/kb"
)

func TestShippedEvaluationSetIsValid(t *testing.T) {
	cases, err := eval.Load(data.EvaluationSet)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := kb.Load("", data.DefaultKnowledgeBase)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range idx.Retrieve("x", 100) {
		ids[d.ID] = true
	}
	for _, c := range cases {
		if c.Source != "" && !ids[c.Source] {
			t.Errorf("%q names unknown source %q", c.Question, c.Source)
		}
	}
}

// Guards retrieval quality: a regression in tokenising or scoring fails CI.
func TestRetrievalQualityFloor(t *testing.T) {
	cases, _ := eval.Load(data.EvaluationSet)
	idx, _ := kb.Load("", data.DefaultKnowledgeBase)
	r := eval.Retrieval(idx, cases, 3)
	if r.Answerable < 15 {
		t.Fatalf("only %d answerable cases", r.Answerable)
	}
	if got := float64(r.TopK) / float64(r.Answerable); got < 0.9 {
		t.Errorf("top-3 hit rate %.2f below 0.90; misses: %v", got, r.Misses)
	}
	if got := float64(r.Top1) / float64(r.Answerable); got < 0.7 {
		t.Errorf("top-1 hit rate %.2f below 0.70", got)
	}
}

func TestLoadRejectsBadSets(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":       `{`,
		"empty":          `[]`,
		"bad expect":     `[{"question":"q","expect":"MAYBE"}]`,
		"no question":    `[{"question":" ","expect":"INCORRECT"}]`,
		"correct no src": `[{"question":"q","expect":"CORRECT"}]`,
	} {
		if _, err := eval.Load([]byte(raw)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestAcceptable(t *testing.T) {
	cases := []struct {
		expect, verdict string
		want            bool
	}{
		{"CORRECT", "CORRECT", true}, {"CORRECT", "AMBIGUOUS", true}, {"CORRECT", "INCORRECT", false},
		{"INCORRECT", "INCORRECT", true}, {"INCORRECT", "AMBIGUOUS", false}, {"INCORRECT", "CORRECT", false},
	}
	for _, c := range cases {
		if got := eval.Acceptable(c.expect, c.verdict); got != c.want {
			t.Errorf("%s/%s = %v", c.expect, c.verdict, got)
		}
	}
}
