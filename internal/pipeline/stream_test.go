package pipeline_test

import (
	"context"
	"reflect"
	"testing"

	"crag/internal/pipeline"
)

// collect runs AskStream and records every event. It uses no lock on purpose:
// the contract is that emit is never called concurrently, and -race checks it.
func collect(t *testing.T, p *pipeline.Pipeline, q string) ([]pipeline.Event, *pipeline.Result) {
	t.Helper()
	var events []pipeline.Event
	res, err := p.AskStream(context.Background(), q, func(e pipeline.Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	return events, res
}

func types(events []pipeline.Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func find(events []pipeline.Event, typ string) *pipeline.Event {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

func TestStreamAmbiguousReportsEveryStepInOrder(t *testing.T) {
	p, _, _ := setup(t, "partial", oneResult)
	events, res := collect(t, p, "Tatkal question")

	got := types(events)
	if len(got) != 6 || got[0] != pipeline.EventRetrieved || got[1] != pipeline.EventGraded || got[5] != pipeline.EventGenerating {
		t.Fatalf("order: %v", got)
	}
	middle := map[string]bool{got[2]: true, got[3]: true, got[4]: true}
	for _, want := range []string{pipeline.EventRewritten, pipeline.EventSearched, pipeline.EventRefined} {
		if !middle[want] {
			t.Fatalf("missing %s in %v", want, got)
		}
	}

	retrieved := find(events, pipeline.EventRetrieved)
	if len(retrieved.Passages) != 3 || retrieved.Passages[0].Label != "" || retrieved.Passages[0].Text == "" {
		t.Fatalf("retrieved should carry unlabelled passages with text: %+v", retrieved.Passages)
	}
	graded := find(events, pipeline.EventGraded)
	if graded.Verdict != "AMBIGUOUS" || !reflect.DeepEqual(graded.Route, []string{"kb", "web"}) || graded.Passages[0].Label != "partial" {
		t.Fatalf("graded: %+v", graded)
	}
	if q := find(events, pipeline.EventRewritten).WebQuery; q == "" || q != res.WebQuery {
		t.Fatalf("rewritten query %q vs result %q", q, res.WebQuery)
	}
	if n := find(events, pipeline.EventSearched).Count; n != 1 {
		t.Fatalf("searched count %d", n)
	}
	if n := find(events, pipeline.EventRefined).Count; n != 1 {
		t.Fatalf("refined count %d", n)
	}
	for i := 1; i < len(events); i++ {
		if events[i].MS < events[i-1].MS {
			t.Fatalf("ms went backwards: %v", events)
		}
	}
	if i, j := indexOf(got, pipeline.EventRewritten), indexOf(got, pipeline.EventSearched); i > j {
		t.Fatalf("query must be written before the search finishes: %v", got)
	}
}

func TestStreamRoutesMatchTheVerdict(t *testing.T) {
	cases := map[string]struct {
		label string
		route []string
		skips []string
	}{
		"correct":   {"relevant", []string{"kb"}, []string{pipeline.EventRewritten, pipeline.EventSearched}},
		"incorrect": {"irrelevant", []string{"web"}, []string{pipeline.EventRefined}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, _, _ := setup(t, c.label, oneResult)
			events, _ := collect(t, p, "What time does Tatkal booking open for AC classes?")
			if got := find(events, pipeline.EventGraded).Route; !reflect.DeepEqual(got, c.route) {
				t.Fatalf("route %v want %v", got, c.route)
			}
			for _, skipped := range c.skips {
				if find(events, skipped) != nil {
					t.Fatalf("%s should not run: %v", skipped, types(events))
				}
			}
		})
	}
}

func TestStreamSkipsGeneratingWhenThereIsNothingToSay(t *testing.T) {
	p, _, _ := setup(t, "irrelevant", nil)
	events, res := collect(t, p, "Unanswerable question")
	if res.Answer != pipeline.NoAnswer || find(events, pipeline.EventGenerating) != nil {
		t.Fatalf("answer %q events %v", res.Answer, types(events))
	}
	if find(events, pipeline.EventSearched) == nil {
		t.Fatal("the search still happened and should be reported, with a count of zero")
	}
}

func TestAskStreamAcceptsNilEmit(t *testing.T) {
	p, _, _ := setup(t, "relevant", nil)
	if _, err := p.AskStream(context.Background(), "What time does Tatkal booking open for AC classes?", nil); err != nil {
		t.Fatal(err)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
