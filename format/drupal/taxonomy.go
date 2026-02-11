package drupal

import (
	"encoding/json"
	"fmt"
	"os"
)

// TaxonomyStore holds taxonomy term mappings for resolution.
type TaxonomyStore struct {
	// terms maps term IDs to term names
	terms map[string]string

	// nodes maps node IDs to node titles
	nodes map[string]string

	// vocabularies maps term IDs to vocabulary names
	vocabularies map[string]string
}

// NewTaxonomyStore creates an empty taxonomy store.
func NewTaxonomyStore() *TaxonomyStore {
	return &TaxonomyStore{
		terms:        make(map[string]string),
		nodes:        make(map[string]string),
		vocabularies: make(map[string]string),
	}
}

// LoadTaxonomyFile loads taxonomy terms from a JSON file.
// The file can be in various formats - we try to detect and parse it.
func LoadTaxonomyFile(path string) (*TaxonomyStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading taxonomy file: %w", err)
	}

	store := NewTaxonomyStore()

	// Try parsing as array of objects with tid and name
	var arrayFormat []struct {
		TID        any    `json:"tid"`
		TermID     any    `json:"term_id"`
		ID         any    `json:"id"`
		Name       string `json:"name"`
		Title      string `json:"title"`
		Label      string `json:"label"`
		Vocabulary string `json:"vocabulary"`
		Vid        string `json:"vid"`
		Type       string `json:"type"` // "taxonomy_term" or "node"
	}

	if err := json.Unmarshal(data, &arrayFormat); err == nil {
		for _, item := range arrayFormat {
			id := extractID(item.TID, item.TermID, item.ID)
			name := extractName(item.Name, item.Title, item.Label)

			if id == "" || name == "" {
				continue
			}

			if item.Type == "node" {
				store.nodes[id] = name
			} else {
				store.terms[id] = name
				if vocab := extractName(item.Vocabulary, item.Vid, ""); vocab != "" {
					store.vocabularies[id] = vocab
				}
			}
		}
		return store, nil
	}

	// Try parsing as object map: {"tid": "name", ...}
	var mapFormat map[string]string
	if err := json.Unmarshal(data, &mapFormat); err == nil {
		for id, name := range mapFormat {
			store.terms[id] = name
		}
		return store, nil
	}

	// Try parsing as nested map: {"terms": {...}, "nodes": {...}}
	var nestedFormat struct {
		Terms map[string]string `json:"terms"`
		Nodes map[string]string `json:"nodes"`
	}
	if err := json.Unmarshal(data, &nestedFormat); err == nil {
		for id, name := range nestedFormat.Terms {
			store.terms[id] = name
		}
		for id, name := range nestedFormat.Nodes {
			store.nodes[id] = name
		}
		return store, nil
	}

	return nil, fmt.Errorf("could not parse taxonomy file format")
}

// Resolve returns the term name for a taxonomy term ID.
func (ts *TaxonomyStore) Resolve(termID string, vocabulary string) (string, bool) {
	if ts == nil || ts.terms == nil {
		return "", false
	}
	name, ok := ts.terms[termID]
	return name, ok
}

// ResolveNode returns the node title for a node ID.
func (ts *TaxonomyStore) ResolveNode(nodeID string) (string, bool) {
	if ts == nil || ts.nodes == nil {
		return "", false
	}
	name, ok := ts.nodes[nodeID]
	return name, ok
}

// AddTerm adds a term to the store.
func (ts *TaxonomyStore) AddTerm(id, name, vocabulary string) {
	ts.terms[id] = name
	if vocabulary != "" {
		ts.vocabularies[id] = vocabulary
	}
}

// AddNode adds a node to the store.
func (ts *TaxonomyStore) AddNode(id, title string) {
	ts.nodes[id] = title
}

// GetVocabulary returns the vocabulary for a term ID.
func (ts *TaxonomyStore) GetVocabulary(termID string) (string, bool) {
	if ts == nil || ts.vocabularies == nil {
		return "", false
	}
	vocab, ok := ts.vocabularies[termID]
	return vocab, ok
}

// TermCount returns the number of terms in the store.
func (ts *TaxonomyStore) TermCount() int {
	if ts == nil {
		return 0
	}
	return len(ts.terms)
}

// NodeCount returns the number of nodes in the store.
func (ts *TaxonomyStore) NodeCount() int {
	if ts == nil {
		return 0
	}
	return len(ts.nodes)
}

// PassthroughResolver is a resolver that returns IDs as-is (no resolution).
type PassthroughResolver struct{}

// Resolve returns the term ID as the name.
func (p *PassthroughResolver) Resolve(termID string, vocabulary string) (string, bool) {
	return termID, true
}

// ResolveNode returns the node ID as the title.
func (p *PassthroughResolver) ResolveNode(nodeID string) (string, bool) {
	return nodeID, true
}

// Helper to extract ID from various field names
func extractID(ids ...any) string {
	for _, id := range ids {
		switch v := id.(type) {
		case float64:
			return fmt.Sprintf("%.0f", v)
		case int:
			return fmt.Sprintf("%d", v)
		case string:
			if v != "" {
				return v
			}
		}
	}
	return ""
}

// Helper to extract name from various field names
func extractName(names ...string) string {
	for _, name := range names {
		if name != "" {
			return name
		}
	}
	return ""
}
