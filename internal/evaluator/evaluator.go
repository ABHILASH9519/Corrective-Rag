// Package evaluator grades each retrieved passage, then picks one CRAG action.
package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"crag/internal/kb"
	"crag/internal/llm"
)

// Labels a passage can receive.
const (
	Relevant   = "relevant"
	Partial    = "partial"
	Irrelevant = "irrelevant"
)

// Verdicts, i.e. the three CRAG actions.
const (
	Correct   = "CORRECT"
	Ambiguous = "AMBIGUOUS"
	Incorrect = "INCORRECT"
)

const system = `You grade passages retrieved for a help desk question.
For each passage choose one label:
- relevant: the passage alone fully answers the question.
- partial: the passage covers part of the question but leaves something out.
- irrelevant: the passage does not help answer this question, even if it shares words with it.
Judge each passage on its own. Do not answer the question.`

var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"grades": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"label": map[string]any{"type": "string", "enum": []string{Relevant, Partial, Irrelevant}},
				},
				"required":             []string{"id", "label"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"grades"},
	"additionalProperties": false,
}

// Grade labels every passage in one batched call and returns {passage id: label}.
// A passage the model skipped, or labelled oddly, counts as irrelevant.
func Grade(ctx context.Context, c llm.Client, question string, docs []kb.Doc) (map[string]string, error) {
	labels := make(map[string]string, len(docs))
	if len(docs) == 0 {
		return labels, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nPassages:\n", question)
	for _, d := range docs {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", d.ID, d.Text)
	}
	raw, err := c.JSON(ctx, system, b.String(), "grades", schema)
	if err != nil {
		return nil, fmt.Errorf("grade passages: %w", err)
	}
	var reply struct {
		Grades []struct{ ID, Label string } `json:"grades"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode grades: %w", err)
	}
	given := map[string]string{}
	for _, g := range reply.Grades {
		given[g.ID] = g.Label
	}
	for _, d := range docs {
		switch l := given[d.ID]; l {
		case Relevant, Partial:
			labels[d.ID] = l
		default:
			labels[d.ID] = Irrelevant
		}
	}
	return labels, nil
}

// Decide turns passage labels into one CRAG action: any relevant -> CORRECT;
// all irrelevant (or nothing retrieved) -> INCORRECT; anything between -> AMBIGUOUS.
func Decide(labels map[string]string) string {
	allIrrelevant := true
	for _, l := range labels {
		if l == Relevant {
			return Correct
		}
		if l != Irrelevant {
			allIrrelevant = false
		}
	}
	if allIrrelevant {
		return Incorrect
	}
	return Ambiguous
}
