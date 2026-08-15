package omeka_s

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// NewSnapshot returns an empty snapshot with the required contract marker and
// current version. Schema resources, content resources, or both must be added
// before the snapshot is valid for canonicalization.
func NewSnapshot() Snapshot {
	return Snapshot{CrosswalkFormat: SnapshotFormat, Version: SnapshotVersion}
}

// CanonicalJSON validates the complete snapshot and returns deterministic JSON.
// Collections are ordered by Omeka resource ID and all JSON-LD object keys are
// canonicalized. This lets acquisition tooling produce stable fingerprints and
// reviewable changes without reimplementing Crosswalk's snapshot contract.
func (snapshot Snapshot) CanonicalJSON() ([]byte, error) {
	totalModel := len(snapshot.Vocabularies) + len(snapshot.Properties) + len(snapshot.ResourceClasses) + len(snapshot.ResourceTemplates)
	totalResources := len(snapshot.ItemSets) + len(snapshot.Items) + len(snapshot.Media)
	if totalModel > maxModelValues {
		return nil, fmt.Errorf("omeka S snapshot model count exceeds %d", maxModelValues)
	}
	if totalResources > maxResources {
		return nil, fmt.Errorf("omeka S snapshot resource count exceeds %d", maxResources)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode Omeka S snapshot: %w", err)
	}
	if int64(len(raw)) > maxInputBytes {
		return nil, fmt.Errorf("omeka S snapshot exceeds %d bytes", maxInputBytes)
	}
	if err := validateJSONShape(raw); err != nil {
		return nil, fmt.Errorf("validate Omeka S snapshot JSON: %w", err)
	}
	decoded, err := decodeSnapshot(raw)
	if err != nil {
		return nil, fmt.Errorf("validate Omeka S snapshot: %w", err)
	}
	snapshot.CrosswalkFormat = SnapshotFormat
	snapshot.Version = SnapshotVersion
	snapshot.SourceID = strings.TrimSpace(snapshot.SourceID)
	snapshot.SourceURI = decoded.provenance.SourceURI
	if decoded.provenance.RetrievedAt != nil {
		snapshot.RetrievedAt = decoded.provenance.RetrievedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	for _, collection := range []struct {
		name   string
		values *[]json.RawMessage
	}{
		{name: "vocabularies", values: &snapshot.Vocabularies},
		{name: "properties", values: &snapshot.Properties},
		{name: "resource_classes", values: &snapshot.ResourceClasses},
		{name: "resource_templates", values: &snapshot.ResourceTemplates},
		{name: "item_sets", values: &snapshot.ItemSets},
		{name: "items", values: &snapshot.Items},
		{name: "media", values: &snapshot.Media},
	} {
		canonical, err := canonicalSnapshotCollection(*collection.values)
		if err != nil {
			return nil, fmt.Errorf("canonicalize %s: %w", collection.name, err)
		}
		*collection.values = canonical
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode canonical Omeka S snapshot: %w", err)
	}
	return canonical, nil
}

func canonicalSnapshotCollection(values []json.RawMessage) ([]json.RawMessage, error) {
	type entry struct {
		id  int64
		raw json.RawMessage
	}
	entries := make([]entry, 0, len(values))
	for index, raw := range values {
		object, err := decodeObject(raw)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", index, err)
		}
		id, err := requiredPositiveID(object, "o:id")
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", index, err)
		}
		canonical, err := canonicalRaw(raw)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", index, err)
		}
		entries = append(entries, entry{id: id, raw: canonical})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].id != entries[j].id {
			return entries[i].id < entries[j].id
		}
		return string(entries[i].raw) < string(entries[j].raw)
	})
	result := make([]json.RawMessage, len(entries))
	for index, entry := range entries {
		result[index] = entry.raw
	}
	return result, nil
}
