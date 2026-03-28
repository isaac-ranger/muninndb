package auth

import "fmt"

// RecallModePreset is a bundle of recall parameters for common retrieval patterns.
// Zero-value fields mean "do not override caller defaults".
type RecallModePreset struct {
	MaxHops            int
	Threshold          float32
	SemanticSimilarity float32
	FullTextRelevance  float32
	Recency            float32
	DisableACTR        bool
}

// recallModePresets are the canonical recall mode definitions, shared by MCP and REST handlers.
var recallModePresets = map[string]RecallModePreset{
	"semantic": {
		SemanticSimilarity: 0.8,
		FullTextRelevance:  0.2,
		MaxHops:            0,
		Threshold:          0.15,
		DisableACTR:        true,
	},
	"recent": {
		Recency:            0.7,
		SemanticSimilarity: 0.3,
		MaxHops:            1,
		Threshold:          0.2,
	},
	"balanced": {}, // zero value = engine defaults
	"deep": {
		MaxHops:   4,
		Threshold: 0.1,
	},
}

// LookupRecallMode returns the RecallModePreset for the given name.
// Returns an error for unknown mode names.
func LookupRecallMode(name string) (RecallModePreset, error) {
	m, ok := recallModePresets[name]
	if !ok {
		return RecallModePreset{}, fmt.Errorf("unknown recall mode %q: valid modes are semantic, recent, balanced, deep", name)
	}
	return m, nil
}
