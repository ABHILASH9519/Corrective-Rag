// Package kb is the knowledge base: JSON passages indexed with TF-IDF and searched by cosine similarity.
//
// It is pure Go with no external service, so the help desk starts instantly and
// runs offline. Similarity is in [0, 1], where 1 means identical wording.
package kb

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"
)

// Passage is one knowledge base entry.
type Passage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Doc is a retrieved passage and how close it was to the question.
type Doc struct {
	ID         string
	Text       string
	Similarity float64
}

// Index holds the passages and their TF-IDF vectors.
type Index struct {
	passages []Passage
	vectors  []map[string]float64 // L2-normalised TF-IDF per passage
	idf      map[string]float64
}

// Parse validates raw JSON and returns the passages.
func Parse(raw []byte) ([]Passage, error) {
	var passages []Passage
	if err := json.Unmarshal(raw, &passages); err != nil {
		return nil, fmt.Errorf("knowledge base is not valid JSON: %w", err)
	}
	if len(passages) == 0 {
		return nil, errors.New("knowledge base is empty")
	}
	seen := map[string]bool{}
	for i := range passages {
		p := &passages[i]
		p.ID, p.Text = strings.TrimSpace(p.ID), strings.TrimSpace(p.Text)
		if p.ID == "" || p.Text == "" {
			return nil, fmt.Errorf("knowledge base entry #%d needs non-empty id and text", i)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("duplicate knowledge base id: %s", p.ID)
		}
		seen[p.ID] = true
	}
	return passages, nil
}

// Load reads path, or uses fallback when path is empty.
func Load(path string, fallback []byte) (*Index, error) {
	raw := fallback
	if path != "" {
		var err error
		if raw, err = os.ReadFile(path); err != nil {
			return nil, fmt.Errorf("read knowledge base: %w", err)
		}
	}
	passages, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return NewIndex(passages), nil
}

// NewIndex builds the TF-IDF index.
func NewIndex(passages []Passage) *Index {
	idx := &Index{passages: passages, idf: map[string]float64{}}
	tokenised := make([][]string, len(passages))
	df := map[string]int{}
	for i, p := range passages {
		tokenised[i] = Tokenize(p.Text)
		for term := range toSet(tokenised[i]) {
			df[term]++
		}
	}
	n := float64(len(passages))
	for term, d := range df {
		idx.idf[term] = math.Log((n+1)/(float64(d)+1)) + 1 // smoothed, always > 0
	}
	idx.vectors = make([]map[string]float64, len(passages))
	for i, tokens := range tokenised {
		idx.vectors[i] = idx.vectorise(tokens)
	}
	return idx
}

// Len is the number of indexed passages.
func (x *Index) Len() int { return len(x.passages) }

// Retrieve returns the k passages closest to the question, best first.
func (x *Index) Retrieve(question string, k int) []Doc {
	q := x.vectorise(Tokenize(question))
	docs := make([]Doc, 0, len(x.passages))
	for i, p := range x.passages {
		docs = append(docs, Doc{ID: p.ID, Text: p.Text, Similarity: round2(dot(q, x.vectors[i]))})
	}
	sort.SliceStable(docs, func(a, b int) bool { return docs[a].Similarity > docs[b].Similarity })
	if k > 0 && k < len(docs) {
		docs = docs[:k]
	}
	return docs
}

// vectorise makes an L2-normalised TF-IDF vector. Terms unseen in the corpus are ignored.
func (x *Index) vectorise(tokens []string) map[string]float64 {
	vec := map[string]float64{}
	for _, t := range tokens {
		if w, ok := x.idf[t]; ok {
			vec[t] += w
		}
	}
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for t := range vec {
		vec[t] /= norm
	}
	return vec
}

func dot(a, b map[string]float64) float64 {
	if len(b) < len(a) {
		a, b = b, a
	}
	var s float64
	for t, v := range a {
		s += v * b[t]
	}
	return s
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func toSet(tokens []string) map[string]struct{} {
	s := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		s[t] = struct{}{}
	}
	return s
}

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can do does for from has have how i if in into is it
		its me my no not of on or our so than that the their then there these they this to was we were what when where
		which who whom why will with would you your`) {
		stopwords[w] = true
	}
}

// Tokenize lowercases, splits on non-alphanumerics, drops stopwords and stems lightly.
func Tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if stopwords[f] {
			continue
		}
		out = append(out, stem(f))
	}
	return out
}

// stem strips common English suffixes. It is deliberately simple and consistent
// for both passages and questions, which is all retrieval needs.
func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(suf)+3 && strings.HasSuffix(w, suf) {
			return strings.TrimSuffix(w, suf)
		}
	}
	return w
}
