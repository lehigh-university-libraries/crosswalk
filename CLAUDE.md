# Crosswalk Project Guidelines

## Project Status

**This is a greenfield project with no production adoption yet.** Until explicitly stated otherwise:
- **No backward compatibility is required.** Remove old code completely; never leave deprecated shims, aliases, or "keep for backward compat" stubs.
- Make the right design choice now rather than hedging.

---

## File Rules

- **NEVER create `*.md` files unless explicitly asked.** All project documentation belongs here.
- **Keep README.md short:** one-line description, the core problem, basic usage, install instructions, link here.

---

## Go Conventions

- Follow [Effective Go](https://go.dev/doc/effective_go); use `gofmt` before committing
- Standard library first; document why any external dependency is required
- Functions small and single-purpose; utility functions for behavior repeated more than twice
- `net/http` for routing (Go 1.22+ routing is sufficient); no `mux`/`chi`/`gin` unless justified
- Always check and wrap errors: `fmt.Errorf("context: %w", err)`; never `_ = doSomething()`
- `log/slog` for all structured logging; never log secrets/tokens/PII
- Use `context.Context` for cancellation in long-running operations
- Every exported symbol must have a doc comment starting with its name
- Comment the **why**, not the **what**
- `golangci-lint run` must pass before committing
- Table-driven tests for all new features; run `go test -race ./...`

---

## Architecture

### The Problem

Metadata formats split into two categories:

1. **Static schemas** (BibTeX, Dublin Core) — the spec IS the model; trivial to map
2. **Dynamic schemas** (Drupal, CSV, MODS) — the spec defines *how* to define models; fields vary per instance

### Mapping Flow

```
Source Format    Profile/Parser     Hub Record      Serializer     Target Format
─────────────    ──────────────     ──────────      ──────────     ─────────────
Drupal JSON   →  field mappings  →  hubv1.Record →  schema.org  →  JSON-LD
CSV           →  column mappings →  (canonical)  →  CrossRef    →  XML
```

The Hub (`hub/v1/hub.proto`) is the canonical intermediate representation. All data passes through it.

### Protobuf: Hub and Spokes

Use protobuf for the hub and well-defined output formats (schema.org, CrossRef, DataCite, etc.). Use Go maps/profiles for dynamic input formats (Drupal, CSV).

```
hub/v1/hub.proto          # Canonical hub schema
spoke/schemaorg/          # schema.org JSON-LD spoke
spoke/crossref/           # CrossRef deposit spoke
spoke/datacite/           # DataCite metadata spoke
...
```

```bash
buf generate              # Regenerate all Go code from protos
buf breaking --against '.git#branch=main'   # Catch breaking changes
buf lint
```

### Dynamic Schema Support

Dynamic formats use a schema registry (`crosswalk/schema`) for type-aware field extraction. Key rule: **always preserve the original source field type in `SourceType`** so parsers know how to extract values correctly (e.g., Drupal's `typed_relation` vs `edtf` vs `entity_reference`).

### Record Groupings

Some formats group records into containers (journal issue + articles, collection + members). Use `RecordGroup{Container, Members, Type}` where `Type` is one of `issue`, `collection`, `series`, `volume`.

---

## CSV Format

CSV is dynamic: headers define the schema. Key rules:

- **Multi-value delimiter:** ` ; ` (space-semicolon-space) — default and hardcoded for contributors
- **Contributors column:** JSON objects separated by ` ; ` (see Contributor Format below)
- **Complex types:** JSON-encoded in cells (`{"value": "256 pages", "attr0": "page"}`)
- **Column mapping:** header names map to IR fields via profile or built-in defaults

---

## Contributor Format

Contributors are self-contained JSON objects. A single `contributors` CSV column holds all contributor data for a record, with entries separated by ` ; `.

### Name prefix encoding

Role and type are embedded in the `name` field using colon-separated prefix: `{roleCode}:{type}:{name}`

- `roleCode` — MARC relator with namespace, e.g. `relators:cre`, `relators:ths`
- `type` — `person` or `organization`
- `name` — the rest of the string (may itself contain colons)

Examples:
- `relators:cre:person:Example, Avery`
- `relators:ths:person:Sample, Morgan`
- `relators:pbl:organization:Example University Press`

### Full JSON format

```json
{"name":"relators:cre:person:Example, Avery","institution":"Example University","email":"...","status":"Graduate Student"}
{"name":"relators:ths:person:Sample, Morgan","institution":"Example University","status":"Faculty"}
```

Supported fields: `name`, `institution`, `orcid`, `email`, `status`, `url`, `additional_name`, `authority_uri`, `authority_source`, `alumni_of`.

These map to `Contributor` proto fields: `Affiliations`, `Identifiers` (ORCID), `Email`, `Status`, `Url`, `AdditionalName`, `AuthorityUri`, `AuthoritySource`, `AlumniOf`.

The parser accepts both the full JSON format and the plain `relators:cre:person:Name` string format (Islandora workbench style).

---

## Conditional Rules (`crosswalk/rules`)

Rules apply conditional transformations when converting hub records to output formats. Example: `ResourceType=Dataset` → `schema:Dataset`, `ResourceType=Article` → `schema:ScholarlyArticle`.

- YAML-based, stored in `~/.crosswalk/rules/{format}.yaml`
- **First match wins**; use `priority` to override order
- Conditions: `equals`, `contains`, `matches` (regex), `in`, `exists`, `all`, `any`, `not`
- Actions: `set_type`, `set_field`/`set_value`, `map_value`, `skip`
- Rules operate on hub fields, not source fields (hub-centric)

---

## Drupal/Islandora Enrichment

Drupal reconciliation asks JSON:API to include the profile-declared relationships and embeds those returned resources before parsing candidates. Crosswalk does not persist Drupal responses or maintain an on-disk Drupal cache; reconciliation and context validation use live, bounded requests. Context validation deduplicates identical lookups only within one HTTP request. Deployments must therefore account for Drupal availability, latency, and request load, or provide an operator-managed upstream cache with an explicit staleness policy.

- Authority URIs come from `field_authority_link[].uri` and `.source`
- Islandora model types come from `field_external_uri[].uri` (schema.org URIs)
- `target_type_uri` on relations carries the schema.org type for serializers

---

## Hub Validation

Validate hub records with `hub.Validate(record, opts)`. Options control which fields are required and whether identifier/date formats are checked. The hub is the right place to enforce data quality — all data passes through it.

Identifier format rules: DOI `10.XXXX/...`, ORCID `XXXX-XXXX-XXXX-XXXX`, ISSN `XXXX-XXXX`, ISBN 10/13 digits. Common URI prefixes are stripped before validation.

---

## The Extras Field

`extra` (a `google.protobuf.Struct`) holds source-specific data that doesn't map to hub fields. Rules:

- **Temporary home only** — if a field appears in >50% of records, promote it to a hub field
- Keys must be `snake_case` (no spaces)
- Consistent types per key across all records
- Store semantics (ISO dates, URIs), not presentation ("March 15, 2024", "All Rights Reserved")
- Use `crosswalk audit extras` to find promotion candidates and type inconsistencies

---

## WASM / Cross-Language

Go conversion logic compiles to WebAssembly for JS/PHP clients. Entry point: `wasm/main.go`. TypeScript wrapper: `wasm/js/crosswalk.ts`. PHP wrapper: `wasm/php/Crosswalk.php`.
