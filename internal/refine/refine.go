// Package refine breaks passages into sentence strips and keeps only the useful ones.
package refine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"crag/internal/kb"
	"crag/internal/llm"
)

const system = `You filter sentences for a help desk.
For each numbered sentence, set keep to true only if it helps answer the question.
Drop sentences that are on a nearby topic but do not help with this question.`

var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"strips": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":   map[string]any{"type": "string"},
					"keep": map[string]any{"type": "boolean"},
				},
				"required":             []string{"id", "keep"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"strips"},
	"additionalProperties": false,
}

// Strip is one sentence tagged with the passage it came from.
type Strip struct {
	ID   string // s1, s2, ... across all passages
	Text string // "<sentence> [KB:<passage id>]"
}

var sentenceEnd = regexp.MustCompile(`([.!?])\s+`)

// Split cuts every passage into sentences numbered s1, s2, ... across all passages.
func Split(docs []kb.Doc) []Strip {
	var strips []Strip
	for _, d := range docs {
		// Put a newline after each sentence end, then split on it.
		for _, s := range strings.Split(sentenceEnd.ReplaceAllString(d.Text, "$1\n"), "\n") {
			if s = strings.TrimSpace(s); s != "" {
				strips = append(strips, Strip{
					ID:   fmt.Sprintf("s%d", len(strips)+1),
					Text: fmt.Sprintf("%s [KB:%s]", s, d.ID),
				})
			}
		}
	}
	return strips
}

// Refine returns only the strips the model marks as useful, in their original order.
func Refine(ctx context.Context, c llm.Client, question string, docs []kb.Doc) ([]string, error) {
	strips := Split(docs)
	if len(strips) == 0 {
		return nil, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\n", question)
	for _, s := range strips {
		fmt.Fprintf(&b, "%s: %s\n", s.ID, s.Text)
	}
	raw, err := c.JSON(ctx, system, b.String(), "strips", schema)
	if err != nil {
		return nil, fmt.Errorf("refine strips: %w", err)
	}
	var reply struct {
		Strips []struct {
			ID   string `json:"id"`
			Keep bool   `json:"keep"`
		} `json:"strips"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode strips: %w", err)
	}
	keep := map[string]bool{}
	for _, s := range reply.Strips {
		if s.Keep {
			keep[s.ID] = true
		}
	}
	var out []string
	for _, s := range strips {
		if keep[s.ID] {
			out = append(out, s.Text)
		}
	}
	return out, nil
}
