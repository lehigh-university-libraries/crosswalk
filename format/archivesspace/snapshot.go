package archivesspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// EncodeSnapshot writes a validated snapshot using canonical JSON object-key
// order and the caller-supplied record/tree order. It does not acquire or
// resolve any ArchivesSpace data.
func EncodeSnapshot(writer io.Writer, snapshot *Snapshot) error {
	if writer == nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: writer is required")
	}
	if snapshot == nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: snapshot is required")
	}
	copy := *snapshot
	copy.SourceID = strings.TrimSpace(copy.SourceID)
	if copy.SourceURI != "" {
		safe, err := safeSourceURI(copy.SourceURI, "")
		if err != nil {
			return fmt.Errorf("encoding ArchivesSpace snapshot source URI: %w", err)
		}
		copy.SourceURI = safe
	}
	if copy.RetrievedAt != "" {
		retrievedAt, err := time.Parse(time.RFC3339Nano, copy.RetrievedAt)
		if err != nil {
			return fmt.Errorf("encoding ArchivesSpace snapshot retrieval time: %w", err)
		}
		copy.RetrievedAt = retrievedAt.UTC().Format(time.RFC3339Nano)
	}
	copy.Records = make([]json.RawMessage, len(snapshot.Records))
	for index, record := range snapshot.Records {
		canonical, err := canonicalJSON(record)
		if err != nil {
			return fmt.Errorf("encoding ArchivesSpace snapshot record %d: %w", index+1, err)
		}
		copy.Records[index] = canonical
	}
	raw, err := json.Marshal(&copy)
	if err != nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: %w", err)
	}
	if _, err := (&Format{}).ParseDataset(bytes.NewReader(raw), nil); err != nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: %w", err)
	}
	indented := bytes.NewBuffer(make([]byte, 0, len(raw)+1))
	if err := json.Indent(indented, raw, "", "  "); err != nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: indent JSON: %w", err)
	}
	if err := indented.WriteByte('\n'); err != nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: terminate JSON: %w", err)
	}
	written, err := writer.Write(indented.Bytes())
	if err != nil {
		return fmt.Errorf("encoding ArchivesSpace snapshot: write output: %w", err)
	}
	if written != indented.Len() {
		return fmt.Errorf("encoding ArchivesSpace snapshot: write output: %w", io.ErrShortWrite)
	}
	return nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSONModel object: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSONModel record must be an object")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSONModel object: %w", err)
	}
	return canonical, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("JSONModel record contains multiple top-level values")
	}
	return fmt.Errorf("decode trailing JSONModel data: %w", err)
}
