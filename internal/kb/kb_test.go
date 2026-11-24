package kb_test

import (
	"testing"

	"crag/data"
	"crag/internal/kb"
)

func shipped(t *testing.T) *kb.Index {
	t.Helper()
	idx, err := kb.Load("", data.DefaultKnowledgeBase)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestShippedKnowledgeBase(t *testing.T) {
	if n := shipped(t).Len(); n != 15 {
		t.Fatalf("want 15 passages, got %d", n)
	}
}

func TestRetrieveFindsRightPassage(t *testing.T) {
	idx := shipped(t)
	cases := map[string]string{
		"What time does Tatkal booking open for AC classes?":              "tatkal-timing",
		"How many kg of free luggage can I take in sleeper?":              "luggage-free",
		"Is there a refund if I cancel a confirmed Tatkal ticket?":        "tatkal-refund",
		"What is an RAC ticket?":                                          "rac",
		"How many days before travel does general reservation open?":      "advance-booking",
		"The train ran 5 hours late and I did not travel, how to refund?": "tdr",
	}
	for q, want := range cases {
		docs := idx.Retrieve(q, 3)
		found := false
		for _, d := range docs {
			if d.ID == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: %q not in top 3 (%v)", q, want, docs)
		}
	}
}

func TestRetrieveOrderAndK(t *testing.T) {
	docs := shipped(t).Retrieve("tatkal aadhaar otp", 3)
	if len(docs) != 3 {
		t.Fatalf("want 3 docs, got %d", len(docs))
	}
	for i := 1; i < len(docs); i++ {
		if docs[i].Similarity > docs[i-1].Similarity {
			t.Fatal("docs not sorted best first")
		}
	}
	if docs[0].ID != "tatkal-aadhaar" {
		t.Errorf("best = %s", docs[0].ID)
	}
}

func TestOffTopicQuestionScoresLow(t *testing.T) {
	on := shipped(t).Retrieve("What time does Tatkal booking open for AC classes?", 1)[0].Similarity
	off := shipped(t).Retrieve("Which stadium hosts the cricket final?", 1)[0].Similarity
	if off >= on {
		t.Fatalf("off-topic %.2f should score below on-topic %.2f", off, on)
	}
}

func TestParseValidation(t *testing.T) {
	bad := map[string]string{
		"not json":  `{`,
		"empty":     `[]`,
		"no text":   `[{"id":"a"}]`,
		"blank id":  `[{"id":" ","text":"x"}]`,
		"duplicate": `[{"id":"a","text":"x"},{"id":"a","text":"y"}]`,
	}
	for name, raw := range bad {
		if _, err := kb.Parse([]byte(raw)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	got, err := kb.Parse([]byte(`[{"id":"a","text":" hello "}]`))
	if err != nil || got[0].Text != "hello" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := kb.Load("/nonexistent/kb.json", nil); err == nil {
		t.Fatal("expected error")
	}
}
