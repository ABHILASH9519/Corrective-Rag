package refine_test

import (
	"context"
	"reflect"
	"testing"

	"crag/internal/kb"
	"crag/internal/llmtest"
	"crag/internal/refine"
)

func TestSplitNumbersAcrossDocsAndTagsSource(t *testing.T) {
	docs := []kb.Doc{{ID: "one", Text: "First. Second! Third? Rs 2.50 is fine."}, {ID: "two", Text: "Other."}}
	var got []string
	for _, s := range refine.Split(docs) {
		got = append(got, s.ID+"="+s.Text)
	}
	want := []string{
		"s1=First. [KB:one]", "s2=Second! [KB:one]", "s3=Third? [KB:one]",
		"s4=Rs 2.50 is fine. [KB:one]", "s5=Other. [KB:two]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestRefineKeepsOnlyMarkedInOrder(t *testing.T) {
	f := &llmtest.Fake{JSONReply: map[string]string{
		"strips": `{"strips":[{"id":"s2","keep":true},{"id":"s1","keep":false},{"id":"s3","keep":true}]}`,
	}}
	docs := []kb.Doc{{ID: "x", Text: "Nope. Yes. Also yes."}}
	got, err := refine.Refine(context.Background(), f, "q", docs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Yes. [KB:x]", "Also yes. [KB:x]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestRefineNoDocsMakesNoCall(t *testing.T) {
	f := &llmtest.Fake{}
	got, err := refine.Refine(context.Background(), f, "q", nil)
	if err != nil || got != nil || len(f.Calls) != 0 {
		t.Fatalf("got %v %v %v", got, err, f.Calls)
	}
}
