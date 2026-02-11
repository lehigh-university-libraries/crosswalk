# Contributing to Crosswalk

This guide covers the architecture, data model, and how to extend crosswalk.

## Architecture Overview

Crosswalk uses a **hub-and-spoke architecture** for metadata conversion:

```
                    ┌─────────────┐
    Drupal JSON ───▶│             │───▶ schema.org JSON-LD
           CSV ───▶│    Hub      │───▶ DataCite XML
        BibTeX ───▶│  (hubv1)    │───▶ MODS XML
      DataCite ───▶│             │───▶ BibTeX
                    └─────────────┘
```

**Why a hub?**
- **N formats, 2N converters** instead of N² pairwise converters
- **Single source of truth** for field semantics
- **Round-trip preservation** through consistent intermediate representation
- **Validation checkpoint** for data quality

All conversions flow: `Source → Hub → Target`

## Dynamic vs Static Formats

Formats fall into two categories:

### Static Formats
The spec **is** the model. Fields are fixed by the standard.
- BibTeX, Dublin Core, DataCite, CSL-JSON
- Implementation: Define proto, write converter, done

### Dynamic Formats
The spec defines **how** to define models. Fields vary per instance.
- Drupal/Islandora (schema varies per site)
- CSV (headers define fields)
- Implementation: Use **profiles** to map source fields to hub

## Profiles: Mapping Dynamic Schemas

Profiles define how source fields map to hub fields for dynamic formats.

### Profile Location

```
~/.crosswalk/profiles/
├── my-drupal-site.yaml
├── legacy-csv.yaml
└── special-collection.yaml
```

### Profile Format

```yaml
name: my-islandora-site
format: drupal
description: Mapping for my Islandora repository

options:
  multi_value_separator: " ; "
  strip_html: true
  taxonomy_mode: resolve  # resolve, id, or both

fields:
  # Simple text field
  field_description:
    hub: description
    type: text

  # Entity reference with resolution
  field_subject:
    hub: subjects
    type: entity_reference
    resolve: taxonomy_term
    vocabulary: lcsh

  # Typed relation (contributor with role)
  field_linked_agent:
    hub: contributors
    type: typed_relation
    role_field: rel_type

  # Date with EDTF parsing
  field_edtf_date_issued:
    hub: dates
    type: date
    date_type: issued
    parser: edtf

  # Hierarchical relation
  field_member_of:
    hub: relations
    type: entity_reference
    resolve: node
    relation_type: member_of
```

### Field Types

| Type | Description | Hub Target |
|------|-------------|------------|
| `text` | Plain text | string fields |
| `formatted_text` | HTML with optional processing | string fields |
| `entity_reference` | Reference to another entity | subjects, relations |
| `typed_relation` | Reference with role (e.g., contributor) | contributors |
| `date` | Date value | dates |
| `link` | URL with optional title | identifiers, links |
| `integer` | Numeric value | page_count, etc. |
| `boolean` | True/false | boolean fields |

### Vocabulary Mapping

For subjects, specify the vocabulary to enable proper categorization:

```yaml
field_subject_lcsh:
  hub: subjects
  vocabulary: lcsh      # Maps to SUBJECT_VOCABULARY_LCSH

field_subject_local:
  hub: subjects
  vocabulary: local     # Maps to SUBJECT_VOCABULARY_LOCAL

field_genre:
  hub: genres
  vocabulary: aat       # Art & Architecture Thesaurus
```

Supported vocabularies: `lcsh`, `lcnaf`, `mesh`, `fast`, `aat`, `tgn`, `local`, `keywords`

## Hub Schema Design

The hub (`hubv1.Record`) is designed for scholarly metadata with these principles:

### Core Field Categories

| Category | Fields | Notes |
|----------|--------|-------|
| **Identity** | title, full_title, identifiers[] | DOI, ISBN, Handle, etc. |
| **Attribution** | contributors[] | With roles and affiliations |
| **Temporality** | dates[] | With DateType for semantics |
| **Classification** | subjects[], genres[] | With vocabulary enums |
| **Description** | abstract, description, table_of_contents | |
| **Relations** | relations[] | parent, parts, versions |
| **Rights** | rights[] | License, access, copyright |
| **Physical** | dimensions, page_count, duration | |
| **Academic** | degree_info, thesis fields | |
| **Files** | files[] | Path, MIME type, size |

### Subjects: Single Field with Vocabulary

Subjects use a **single repeated field** with a vocabulary enum:

```protobuf
message Subject {
  SubjectType type = 1;           // TOPIC, PERSON, PLACE, etc.
  string value = 2;               // "Machine learning"
  string uri = 3;                 // Authority URI
  SubjectVocabulary vocabulary = 4; // LCSH, AAT, LOCAL, etc.
}
```

**Why not separate fields per vocabulary?**
- Simpler hub schema
- Serializers filter by vocabulary as needed
- No need to update hub when adding vocabularies

Example usage in serializers:
```go
// Get only LCSH subjects
lcsh := hub.GetSubjectsByVocab(record, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH)

// Get all as flat list (for formats without vocabulary support)
all := hub.CollapseSubjects(record.Subjects)
```

### Dates: Semantic Types

Dates include a `DateType` for semantic meaning:

```protobuf
message Date {
  DateType type = 1;    // ISSUED, CREATED, MODIFIED, etc.
  int32 year = 2;
  int32 month = 3;
  int32 day = 4;
  string raw = 5;       // Original string if unparseable
  // ... range fields for date ranges
}
```

### Identifiers: Type-Aware

```protobuf
message Identifier {
  IdentifierType type = 1;  // DOI, ISBN, ORCID, HANDLE, etc.
  string value = 2;
  string display = 3;       // Human-readable form
}
```

### The Extra Field

The `extra` field holds data that doesn't map to standard hub fields:

```protobuf
google.protobuf.Struct extra = 40;
```

**Guidelines:**
- Use for source-specific data that needs round-trip preservation
- Use machine names as keys (snake_case, not "Human Labels")
- Maintain consistent types across records
- If a field appears in >50% of records, consider promoting to hub schema

## Entity Enrichment

For Islandora/Drupal, the enricher fetches referenced entities to resolve:
- Taxonomy term names and authority URIs
- Node titles and types
- Media file information

```bash
# Enrich entity references from live Drupal site
crosswalk convert drupal schemaorg -i data.json --base-url https://example.com
```

The enricher:
1. Detects entity references in JSON
2. Fetches full entity data from Drupal API
3. Caches responses (24h TTL)
4. Extracts authority URIs, schema.org types, etc.

Authority data flows:
```
field_subject[0].target_id → fetch taxonomy term
  → _entity.name = "Machine learning"
  → _entity.field_authority_link[0].uri = "http://id.loc.gov/..."
  → _entity.field_authority_link[0].source = "lcsh"
```

Model types flow:
```
field_member_of[0].target_id → fetch node
  → _entity.field_model[0]._entity.name = "Collection"
  → _entity.field_model[0]._entity.field_external_uri[0].uri = "https://schema.org/Collection"
```

## Round-Trip Preservation

To preserve data through hub conversion:

### 1. Track Source Info

```protobuf
message SourceInfo {
  string format = 1;           // "drupal", "csv"
  string source_id = 3;        // Original ID
  repeated string unmapped_fields = 6;  // Fields that went to extra
}
```

### 2. Store Unmapped in Extra

Fields that don't map to hub go to `extra` with their original names.

### 3. Preserve Vocabulary on Serialize

When serializing back, check subject vocabulary to route correctly.

## Creating a New Spoke Format

### Step 1: Define the Proto Schema

Create `spoke/<format>/v<version>/<format>.proto`:

```protobuf
syntax = "proto3";
package spoke.example.v1;
option go_package = "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/example/v1;examplev1";

import "hub/v1/options.proto";

message Record {
  string title = 1;
  repeated Creator creators = 2;
}

message Creator {
  string name = 1;
}
```

**Note:** Use underscores for semantic versions in directory names: `v5_3_1` not `v5.3.1`.

Generate code:
```bash
make generate
```

### Step 2: Create Format Handler

Create `format/<format>/<format>.go`:

```go
package example

import (
    "bytes"
    "github.com/lehigh-university-libraries/crosswalk/format"
)

type Format struct{}

var (
    _ format.Format     = (*Format)(nil)
    _ format.Serializer = (*Format)(nil)
)

func (f *Format) Name() string         { return "example" }
func (f *Format) Description() string  { return "Example Format" }
func (f *Format) Extensions() []string { return []string{"xml"} }

func (f *Format) CanParse(peek []byte) bool {
    return bytes.Contains(peek, []byte("<example>"))
}

func init() {
    format.Register(&Format{})
}
```

### Step 3: Create Serializer

Use the three-step pattern:

```go
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
    for i, record := range records {
        // Step 1: Hub → Spoke proto
        spoke, err := hubToSpoke(record)
        if err != nil {
            return fmt.Errorf("converting record %d: %w", i, err)
        }

        // Step 2: Spoke proto → Wire format
        xml := spokeToXML(spoke)

        // Step 3: Marshal
        output, err := xml.MarshalIndent(xml, "", "  ")
        if err != nil {
            return fmt.Errorf("marshaling record %d: %w", i, err)
        }
        w.Write(output)
    }
    return nil
}
```

**Why three steps?**
1. `hubToSpoke`: Type-safe mapping with compile-time validation
2. `spokeToXML`: Add format-specific tags/attributes
3. `Marshal`: Standard library serialization

### Alternative: Annotation-Based XML

For XML formats, use proto annotations to skip the wire format step:

```protobuf
message Record {
  option (hub.v1.message) = {
    xml_name: "dc:record"
    xml_namespaces: ["dc=http://purl.org/dc/elements/1.1/"]
  };

  string title = 1 [(hub.v1.field) = {
    xml_name: "dc:title"
  }];
}
```

Then serialize directly:
```go
protoxml.WriteTo(w, spokeRecord)
```

### Subject Handling in Serializers

Different formats have different subject field support:

| Format | Subject Handling |
|--------|-----------------|
| BibTeX | Single `keywords` field - collapse all |
| DataCite | All subjects in one list |
| arXiv | Route by vocabulary (ARXIV→primary, MSC→secondary) |
| MODS | Separate `<subject>` elements with authority attributes |

Use hub helpers:
```go
// All subjects as flat strings
hub.CollapseSubjects(record.Subjects)

// Filter by vocabulary
hub.GetSubjectsByVocab(record, hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH)

// Filter by type
hub.GetSubjectsByType(record, hubv1.SubjectType_SUBJECT_TYPE_TOPIC)
```

## Testing

Add fixtures in `fixtures/<format>/` and verify:
- Parsing extracts expected hub fields
- Serialization produces valid output
- Round-trip preserves data

```bash
go test ./...
go test -race ./...  # Check for race conditions
```

## Checklist

- [ ] Proto schema in `spoke/<format>/v<version>/`
- [ ] Run `make generate`
- [ ] Format handler in `format/<format>/`
- [ ] Serializer with `hubToSpoke()` function
- [ ] Subject vocabulary handling
- [ ] Tests with fixtures
- [ ] `make lint` passes
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
