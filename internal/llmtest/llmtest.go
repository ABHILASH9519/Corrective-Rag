// Package llmtest provides a scriptable fake llm.Client for tests.
package llmtest

import (
	"context"
	"sync"
)

// Fake answers JSON calls by schema name and Text calls with a fixed reply.
type Fake struct {
	mu        sync.Mutex
	JSONReply map[string]string // schema name -> raw JSON reply
	TextReply string
	Err       error
	Calls     []string // "json:<name>" or "text", in call order
}

// JSON implements llm.Client.
func (f *Fake) JSON(_ context.Context, _, _, name string, _ map[string]any) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "json:"+name)
	if f.Err != nil {
		return nil, f.Err
	}
	return []byte(f.JSONReply[name]), nil
}

// Text implements llm.Client.
func (f *Fake) Text(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "text")
	return f.TextReply, f.Err
}

// Count returns how many calls had the given label.
func (f *Fake) Count(label string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c == label {
			n++
		}
	}
	return n
}
