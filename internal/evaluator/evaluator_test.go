package evaluator_test

import (
	"context"
	"errors"
	"testing"

	"crag/internal/evaluator"
	"crag/internal/kb"
	"crag/internal/llmtest"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"any relevant", map[string]string{"a": "relevant", "b": "irrelevant"}, evaluator.Correct},
		{"all irrelevant", map[string]string{"a": "irrelevant", "b": "irrelevant"}, evaluator.Incorrect},
		{"nothing retrieved", map[string]string{}, evaluator.Incorrect},
		{"partial", map[string]string{"a": "partial", "b": "irrelevant"}, evaluator.Ambiguous},
		{"partial beats nothing but relevant wins", map[string]string{"a": "partial", "b": "relevant"}, evaluator.Correct},
	}
	for _, c := range cases {
		if got := evaluator.Decide(c.labels); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestGradeDefaultsMissingAndUnknownToIrrelevant(t *testing.T) {
	f := &llmtest.Fake{JSONReply: map[string]string{
		"grades": `{"grades":[{"id":"a","label":"relevant"},{"id":"c","label":"banana"}]}`,
	}}
	docs := []kb.Doc{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, err := evaluator.Grade(context.Background(), f, "q", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "relevant" || got["b"] != "irrelevant" || got["c"] != "irrelevant" {
		t.Fatalf("got %v", got)
	}
}

func TestGradeNoDocsMakesNoCall(t *testing.T) {
	f := &llmtest.Fake{}
	got, err := evaluator.Grade(context.Background(), f, "q", nil)
	if err != nil || len(got) != 0 || len(f.Calls) != 0 {
		t.Fatalf("got %v err %v calls %v", got, err, f.Calls)
	}
}

func TestGradeErrors(t *testing.T) {
	if _, err := evaluator.Grade(context.Background(), &llmtest.Fake{Err: errors.New("boom")}, "q", []kb.Doc{{ID: "a"}}); err == nil {
		t.Fatal("expected provider error")
	}
	bad := &llmtest.Fake{JSONReply: map[string]string{"grades": "not json"}}
	if _, err := evaluator.Grade(context.Background(), bad, "q", []kb.Doc{{ID: "a"}}); err == nil {
		t.Fatal("expected decode error")
	}
}
