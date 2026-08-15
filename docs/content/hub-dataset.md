# Hub records and datasets

Crosswalk uses two related data contracts. A Hub record represents one
scholarly object. A Dataset binds one or more Hub records to stable local keys,
ordered hierarchy, diagnostics, and acquisition provenance. Format adapters
should preserve information in these contracts before target-specific
serialization begins.

## Hub record

`hubv1.Record` is the canonical metadata record shared by every format spoke.
Its first-class fields cover:

- titles, descriptions, notes, and preferred citations;
- typed contributors and affiliations;
- EDTF-aware dates;
- resource types, subjects, genres, languages, and physical forms;
- publication, rights, access, archival, degree, and funding metadata;
- typed identifiers and relations; and
- primary, supplemental, service, and derivative file metadata.

The Protocol Buffer definition is the authoritative field reference. Generated
Go types and JSON Schemas are derived from it; do not add a second hand-written
wire schema.

### Repeated values

Repeatable source fields stay as ordered lists in the Hub until the target
format is selected. Publisher, publication-place, physical-description,
edition, and language lists also mirror their first value into the older
scalar field for compatibility with existing Hub consumers. Adapters for
repeatable targets emit the full list; formats whose wire standard defines
only one value receive that primary value at serialization time. This keeps a
scalar target limitation from silently discarding values earlier in a
multi-step conversion.

### Record provenance

`source_info` distinguishes the representation from the system that supplied
it:

| Field | Meaning |
|---|---|
| `format` / `format_version` | Input adapter and wire-format version |
| `source_id` | Identifier used by the source system |
| `origin` | Source installation or system, independent of format |
| `source_uri` | Canonical locator for the source record |
| `parsed_at` | Time the source was parsed |
| `profile` | Operator-facing profile name |
| `profile_fingerprint` / `model_fingerprint` | Exact executable mapping and schema snapshot used together |
| `unmapped_fields` | Source fields retained outside first-class Hub fields |

Persisted source URIs must be canonical HTTP(S) URIs without credentials or
fragments. Profile and model fingerprints are a pair: supplying only one is an
invalid provenance claim.

### Identifier identity

An identifier has four independent parts:

- `type` is a broad interoperability hint such as DOI, ORCID, WOS, or local;
- `scheme` is the open canonical machine name and is authoritative for schemes
  not represented by the enum;
- `namespace_uri` scopes the value to its issuing authority or repository; and
- `identity_level` states whether the value identifies a work, version,
  manifestation, concept, or source record.

Equality is strong duplicate evidence only when the active profile explicitly
approves that scheme, namespace, identity level, and lookup selector. A generic
local value is never assumed globally unique. See
[Existing-item reconciliation](reconciliation.md#identifier-first-matching).

### Files

A Hub file may describe a local `path`, a canonical remote `uri`, a distinct
`access_url`, and a checksum plus its algorithm. These are metadata assertions,
not permission to read or fetch a resource. Acquisition clients authorize and
stage remote content; a target specification normalizes emitted paths; and a
deployment-aware operator verifies a local path before repository mutation.

### Source-specific fields

`extra` preserves source semantics that have no first-class Hub field. Keys use
stable machine names, values keep consistent JSON types across a batch, and the
data is not assumed searchable. Presentation labels do not belong in `extra`.
Frequently used fields should be promoted into the Hub instead of becoming an
unreviewed parallel schema.

## Dataset

A Dataset adds batch and hierarchy semantics that do not fit on one record:

| Part | Contract |
|---|---|
| `records` | Hub records bound to unique dataset-local keys |
| `hierarchy.nodes` | An ordered forest of `record_key`, optional `parent_key`, and zero-based sibling `position` |
| `diagnostics` | Structured source, row, column, code, severity, and message data |
| `provenance` | Format, source, safe source URI, retrieval time, and optional profile/model fingerprints for the result set |

Records and hierarchy nodes use canonical depth-first pre-order. Validation
rejects duplicate or unsafe keys, missing records or parents, cycles, duplicate
sibling positions, non-contiguous order, unsafe provenance URIs, incomplete
fingerprint pairs, and oversized datasets. Arbitrary hierarchy depth is
preserved; flattening is a target-serializer decision, not a parsing shortcut.

Adapters that implement dataset parsing or serialization receive this full
contract. Flat adapters are wrapped in a deterministic flat Dataset so every
pipeline can retain stable keys and provenance. ArchivesSpace uses the hierarchy
directly, and Islandora Workbench planning can turn it into upload IDs, parent
IDs, and sibling weights.

## Validation boundary

Hub validation checks canonical metadata invariants. It does not query a live
repository, inspect a context filesystem, create taxonomy terms, or authorize a
network fetch. Those decisions belong respectively to reconciliation,
deployment-aware preflight, and explicit sitectl operations. See
[Architecture](architecture.md#processing-stages) and
[Transformation specifications](specifications.md#validation-rules).
