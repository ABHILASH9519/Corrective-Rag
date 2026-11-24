package pipeline_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"crag/data"
	"crag/internal/kb"
	"crag/internal/llmtest"
	"crag/internal/pipeline"
	"crag/internal/web"
)

// gradedFake returns a Fake whose "grades" reply labels the three passages the
// retriever returns for the question, whatever they are.
type gradingLLM struct {
	*llmtest.Fake
	label string
}

func (g *gradingLLM) JSON(ctx context.Context, system, user, name string, schema map[string]any) ([]byte, error) {
	switch name {
	case "grades":
		var parts []string
		for _, id := range idsIn(user) {
			parts = append(parts, `{"id":"`+id+`","label":"`+g.label+`"}`)
		}
		g.Fake.JSONReply = map[string]string{"grades": `{"grades":[` + strings.Join(parts, ",") + `]}`}
	case "strips":
		g.Fake.JSONReply = map[string]string{"strips": `{"strips":[{"id":"s1","keep":true}]}`}
	}
	return g.Fake.JSON(ctx, system, user, name, schema)
}

// idsIn pulls "[id]" headers out of the evaluator prompt.
func idsIn(prompt string) []string {
	var ids []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			ids = append(ids, line[1:len(line)-1])
		}
	}
	return ids
}

func setup(t *testing.T, label string, results []web.Result) (*pipeline.Pipeline, *gradingLLM, *web.Fake) {
	t.Helper()
	idx, err := kb.Load("", data.DefaultKnowledgeBase)
	if err != nil {
		t.Fatal(err)
	}
	g := &gradingLLM{Fake: &llmtest.Fake{TextReply: "Answer. [KB:tatkal-timing] [WEB:https://example.com]"}, label: label}
	s := &web.Fake{Results: results}
	return pipeline.New(idx, g, s, pipeline.Options{HelpDesk: "Indian Railways"}), g, s
}

var oneResult = []web.Result{{Title: "T", Snippet: "S", URL: "https://example.com"}}

func TestCorrectOnlyRefines(t *testing.T) {
	p, g, s := setup(t, "relevant", oneResult)
	res, err := p.Ask(context.Background(), "What time does Tatkal booking open for AC classes?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "CORRECT" || g.Count("json:strips") != 1 || len(s.Queries) != 0 {
		t.Fatalf("verdict %s strips %d web %v", res.Verdict, g.Count("json:strips"), s.Queries)
	}
	if res.WebQuery != "" || len(res.Passages) != 3 || res.Passages[0].Label != "relevant" {
		t.Fatalf("%+v", res)
	}
}

func TestIncorrectOnlySearchesWeb(t *testing.T) {
	p, g, s := setup(t, "irrelevant", oneResult)
	res, err := p.Ask(context.Background(), "How much baggage on an IndiGo flight?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "INCORRECT" || g.Count("json:strips") != 0 || len(s.Queries) != 1 {
		t.Fatalf("verdict %s strips %d web %v", res.Verdict, g.Count("json:strips"), s.Queries)
	}
	if res.WebQuery == "" || len(res.Knowledge) != 1 || !strings.Contains(res.Knowledge[0], "[WEB:https://example.com]") {
		t.Fatalf("%+v", res)
	}
}

func TestAmbiguousDoesBothAndMergesKBFirst(t *testing.T) {
	p, g, s := setup(t, "partial", oneResult)
	res, err := p.Ask(context.Background(), "Tatkal question")
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "AMBIGUOUS" || g.Count("json:strips") != 1 || len(s.Queries) != 1 {
		t.Fatalf("verdict %s", res.Verdict)
	}
	if len(res.Knowledge) != 2 || !strings.Contains(res.Knowledge[0], "[KB:") || !strings.Contains(res.Knowledge[1], "[WEB:") {
		t.Fatalf("knowledge order: %q", res.Knowledge)
	}
}

func TestNoKnowledgeMeansHonestDontKnow(t *testing.T) {
	p, g, _ := setup(t, "irrelevant", nil)
	res, err := p.Ask(context.Background(), "Unanswerable question")
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != pipeline.NoAnswer || g.Count("text") != 1 { // only the query rewrite, no answer call
		t.Fatalf("answer %q text calls %d", res.Answer, g.Count("text"))
	}
}

func TestWebFailureDoesNotFailTheRequest(t *testing.T) {
	p, _, s := setup(t, "irrelevant", nil)
	s.Err = errors.New("ddg down")
	res, err := p.Ask(context.Background(), "Unanswerable question")
	if err != nil || res.Answer != pipeline.NoAnswer {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestProviderErrorPropagates(t *testing.T) {
	p, g, _ := setup(t, "relevant", nil)
	g.Fake.Err = errors.New("groq down")
	if _, err := p.Ask(context.Background(), "anything here"); err == nil {
		t.Fatal("expected error")
	}
}

func TestEmptyQuestionRejected(t *testing.T) {
	p, _, _ := setup(t, "relevant", nil)
	if _, err := p.Ask(context.Background(), "   "); err == nil {
		t.Fatal("expected error")
	}
}

func TestSources(t *testing.T) {
	got := pipeline.Sources("x [KB:tdr] y [WEB:https://example.com/a?b=1] z [KB:tdr] [not a tag]")
	want := []string{"KB:tdr", "WEB:https://example.com/a?b=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	if pipeline.Sources("no tags") != nil {
		t.Fatal("want nil")
	}
}
