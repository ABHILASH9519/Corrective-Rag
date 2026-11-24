// Package data embeds the default knowledge base and evaluation set so the binary runs anywhere.
package data

import _ "embed"

// DefaultKnowledgeBase is data/knowledge_base.json, compiled into the binary.
// Override it at runtime with CRAG_KB_PATH.
//
//go:embed knowledge_base.json
var DefaultKnowledgeBase []byte

// EvaluationSet is data/evaluation_set.json: labelled questions for `crag eval`.
//
//go:embed evaluation_set.json
var EvaluationSet []byte
