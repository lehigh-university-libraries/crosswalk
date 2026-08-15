# Transformation specifications

A transformation specification is the reviewed, directional contract between a
source table, the Hub, and a target table. It declares mapping and validation
behavior by canonical machine field name. Spreadsheet labels and aliases are
accepted input vocabulary and diagnostic text; they never select a validation
rule.

Use `crosswalk spec compile drupal` to create a draft, edit the draft, and run
`crosswalk spec validate` to validate and seal its fingerprint. For Islandora,
`crosswalk spec contract` derives the independently provisioned sitectl trust
anchor from that sealed specification.

## Top-level contract

| Key | Purpose |
|---|---|
| `version` | Specification schema version understood by Crosswalk |
| `name` | Stable operator-facing contract name |
| `description` | Optional descriptive text |
| `source` | Ordered input table and its mapping rules |
| `target` | Ordered output table and its mapping rules |
| `defaults` | Reviewed workflow policy such as staging roots and supplemental-media terms |
| `fingerprint` | Lowercase SHA-256 over executable specification content |

Source and target tables are deliberately distinct. A human source header is
not a Drupal machine name, and a target column is not inferred from its display
label.

## Map a human-readable sheet

Start with the draft generated from the target site's frozen Drupal model, then
review its `source.fields` entries. For each spreadsheet column:

1. keep a stable canonical `name` for rules and output planning;
2. put the wording users see in `label`, and put accepted historical or local
   spellings in `aliases`;
3. map the column to a canonical `hub` path with the appropriate `codec`; and
4. attach deterministic or contextual `validations` by canonical field name.

Crosswalk resolves an incoming header against this reviewed vocabulary. It does
not infer a Drupal field from similar-looking prose, and a validation rule
cannot refer to a display label. This lets a site rename a Google Sheet column
without coupling its validation policy to that presentation text; add the new
wording as a label or alias, review the resulting specification fingerprint,
and reseal the contract.

## Table schema

Each `source` or `target` table may contain:

| Key | Meaning |
|---|---|
| `format` | Registered source or target format |
| `header_rows` | Number of table header rows |
| `machine_header_row` | Zero-based row containing canonical machine headers |
| `human_header_row` | Zero-based row containing display headers |
| `multi_value_separator` | Exact separator used for repeated cell values |
| `fields` | Ordered field mappings |
| `required_groups` | Operation-specific groups for which at least one canonical field is required |
| `validations` | Cross-field or cross-row rules |

A field mapping supports these keys:

| Key | Meaning |
|---|---|
| `name` | Canonical machine name used by rules and target output |
| `label`, `schema_label`, `description`, `aliases` | Human-facing input and review metadata |
| `hub` | Canonical Hub path |
| `codec` | Typed representation used while parsing or serializing |
| `profile_rule` | Exact identifier rule from the bound system profile |
| `source_type`, `settings`, `instance_settings` | Modeled source-system field metadata |
| `cardinality` | Maximum values in the source cell; zero is unbounded |
| `required` | Required for every applicable operation |
| `required_for` | Operations for which the field is required |
| `optional_for_object_models` | Mapping-declared exception for exact source model values |
| `default` | Explicit default value |
| `operations` | Operations in which the field participates |
| `validations` | Rules applied to this field |

Supported Workbench operations are `create`, `update`, `add_media`, `agents`,
`pending_supplemental`, and `unpublished_supplemental`. The valid codecs are
validated when the spec is sealed; commonly used codecs include `string`,
`multi`, `file`, `contributors`, `boolean`, `integer`, `unsigned`, `edtf`, and
`profile_identifier`. `ignore` explicitly discards a declared column. Composite
target fields use codecs such as `typed_relation`, `attributes`, `typed`, and
`json`.

## Required fields and groups

`required` and `required_for` apply after Crosswalk infers the row operation.
For example, a title can be required for `create` without making it mandatory in
a partial `update`. A `required_group` names canonical fields and the operations
for which at least one must be populated. These policies are part of the sealed
mapping; the parser does not infer them from labels.

Cardinality is evaluated against the table's declared
`multi_value_separator`, independently of the Hub codec, so a scalar Drupal
field containing two separator-delimited values still exceeds cardinality one.
Empty segments do not count. The canonical Workbench `title` field is the one
intentional scalar-text exception because a title may contain the separator;
this exception is selected by canonical field name, never by its label or
alias.

The built-in and Drupal-compiled Workbench contracts require the canonical `id`
(`Upload ID`) on every create row. It is the stable within-batch key used for
hierarchy, supplemental reconciliation, and deterministic artifacts; update and
add-media rows instead use `node_id` according to their operation policy.

## Validation rules

Validation is split into two phases:

- `deterministic` rules depend only on the table, sealed specification, bound
  profile, and Hub record. The phase may be omitted because deterministic is the
  default.
- `context` rules require a selected deployment, filesystem, or external
  authority. They are declared in the mapping so an operator can plan them, but
  they must be executed through an allowlisted deployment-aware integration.
  The standalone Workbench CLI does not install one. `crosswalk serve` installs
  Crosswalk's fixed-origin resolver only when both the exact Drupal profile and
  an operator-selected Drupal JSON:API root are configured; a rule declaration
  alone never grants access.

Library integrations can install a trusted implementation of
`validationcontext.Resolver` (also exposed as
`httpapi.ValidationContextResolver`) with
`CrosswalkEngine.ConfigureValidationContext`. The engine then passes only a
parsed node ID, a specification-normalized staging path, a numeric Getty TGN
identifier, or a typed entity/allowed-value query to the resolver. The entity
query contains a model-declared entity type and bundles plus an already parsed
ID, name, or URI value. The allowed-value query contains the canonical field,
source type, frozen callback name, candidate, operation identity, exact
profile/model fingerprints, and a bounded canonical row projection. These
values are inert query data: the resolver chooses one fixed selected site and
approved authority endpoints and must never execute a callback name locally or
treat a spreadsheet URI as a network destination. Resolver errors fail the
check instead of silently downgrading it to deterministic-only validation.
Callers can use `HasValidationContext` when contextual parity is mandatory.

Field rules are attached to one canonical field:

| Rule | Parameters | Meaning |
|---|---|---|
| `max_runes` | positive `limit` | Maximum Unicode code points |
| `absolute_http_url` | none | Absolute HTTP or HTTPS URI syntax |
| `workbench_link` | none | Absolute HTTP(S) URL, optionally followed by `%%` and a display label |
| `geolocation` | none | Finite `latitude,longitude` pair in geographic bounds; a Workbench escape backslash is accepted |
| `authority_link` | non-empty `values` | Exact configured authority source, URL, and optional title encoded as `source%%URL%%title` |
| `media_track` | none | `label:kind:language:file.vtt` with an allowed WebVTT track kind and language syntax |
| `entity_reference` | optional `bundles` | Unsigned ID, absolute HTTP(S) URI, or bundle-qualified/name reference using the configured target bundles |
| `typed_relation` | optional exact relator `values` and target `bundles` | `namespace:code:reference` using mapping-declared relators and entity-reference grammar |
| `doi` | none | DOI syntax and canonical identifier policy |
| `rights_statement` | none | Known rights-statement value or URI |
| `no_line_breaks` | none | Reject CR/LF in taxonomy or multi-value cells |
| `enum` | non-empty `values`, optional `case_insensitive` | Exact mapping-owned value set |
| `finite_number` | none | Numeric value excluding NaN and infinities |
| `numeric_range` | `minimum`, `maximum`, or both | Inclusive finite numeric bounds |
| `pattern` | fully anchored `pattern` | Go regular expression, beginning with `^` and ending with `$`, applied to the canonicalized profile value when profile-bound |
| `not_future_timestamp` | none | RFC 3339 timestamp that is not later than the current time |
| `media_extension` | non-empty `media_types` | Select a sealed media type by filename extension, then require an extension allowed by that type |
| `contributor` | none | Contributor structure, type, role, and person-only metadata policy |
| `getty_tgn` | none | Canonical Getty TGN reference syntax |
| `context_node_exists` | `phase: context` | Referenced Drupal node exists in the selected site |
| `context_entity_exists` | `phase: context`, `entity_type`, optional `bundles`, `handler`, and taxonomy-only `allow_new_names` | Typed entity reference exists on the fixed selected site within the model-declared bundles |
| `context_allowed_value` | `phase: context`, `provider` | Candidate is allowed by the frozen Drupal `allowed_values_function`; the shipped resolver verifies the binding and fails explicitly until the selected site provides a fixed adapter |
| `context_file_readable` | `phase: context` | File is an allowed readable regular file for the context user |
| `context_tgn_resolves` | `phase: context` | Getty authority lookup succeeds through the approved resolver |
| `context_url_alias_available` | `phase: context` | URL alias is unused on the fixed selected Drupal site; the alias is inert path data and cannot select a network origin |

The Drupal composite cell grammars are deliberately exact:

- `workbench_link` accepts `URL` or `URL%%label`; the URL must be absolute
  HTTP(S) with no user information, while the optional label remains display
  data.
- `geolocation` accepts an optional leading Workbench escape backslash followed
  by finite `latitude,longitude`, with latitude from -90 through 90 and
  longitude from -180 through 180.
- `authority_link` accepts `source%%URL` or `source%%URL%%title`; `source` must
  exactly match the frozen `authority_sources` list and the URL must be absolute
  HTTP(S).
- `media_track` accepts `label:kind:language:file.vtt`. The label is non-empty;
  kind is exactly `subtitles`, `descriptions`, `metadata`, `captions`, or
  `chapters`; language is a two- or three-letter code with optional
  alphanumeric subtags; and the filename ends in `.vtt` case-insensitively.
- `entity_reference` accepts an unsigned numeric ID, an absolute HTTP(S) URI,
  or a name of at most 255 Unicode code points. With several configured bundles
  a name must be `bundle:name`; with one bundle either `name` or that exact
  `bundle:name` form is accepted; with no bundles only an unqualified name is
  accepted.
- `typed_relation` prefixes that entity-reference grammar with two alphanumeric
  relator components: `namespace:code:reference`. When `rel_types` and target
  bundles are present in the model, both are copied into the sealed rule and
  matched exactly.

`media_extension` does not point to another canonical field. Its
`media_types` list is the complete, fingerprinted policy used for that cell.
Each entry has a machine `media_type`, zero or more lower-case extension names
without dots in `select_extensions`, the corresponding sealed
`allowed_extensions`, and an optional `fallback`. Selector extensions may not
overlap, and exactly one entry must be the fallback. Crosswalk selects the
first media type whose `select_extensions` contains the filename extension, or
the fallback when none matches, and then requires that extension to appear in
the selected entry's `allowed_extensions`.

For the primary, published supplemental, and unpublished supplemental
Workbench path fields, Drupal compilation starts with Crosswalk's fixed table
of standard Workbench selectors. It reads `allowed_extensions` only from the
exact standard file field for each standard media bundle:

| Media bundle | Workbench file field |
|---|---|
| `image` | `field_media_image` |
| `document` | `field_media_document` |
| `file` | `field_media_file` |
| `audio` | `field_media_audio_file` |
| `video` | `field_media_video_file` |
| `extracted_text` | `field_media_file` |

Another file-valued field in one of those bundles is not a substitute, and a
custom media bundle is not automatically added to the policy. A site that
needs a custom bundle or filename selector must edit the draft's operational
`media_extension` rules to supply the complete `media_types` list, including
exactly one fallback, and then validate and reseal the specification.

A directly mapped Drupal `file`, `image`, or `media_track` field that declares
`file_extensions` instead receives a one-entry policy whose allowed extensions
come from that exact field and whose sole entry is the fallback. The generated
or explicitly reviewed policy is fingerprinted; neither an object-model label
nor another spreadsheet column selects it at runtime.

Drupal's authored `created` base field receives both its exact
`YYYY-MM-DDTHH:MM:SS±HH:MM` pattern and `not_future_timestamp`. The generated
`langcode` field receives an exact enum containing Workbench's supported Drupal
language codes. A non-empty `url_alias` must start with `/`, contain no line
break, and be unique among the other non-empty aliases in that input sheet.
That table rule does not query Drupal for an alias already used outside the
sheet.

Drupal compilation derives deterministic rules only when the frozen model has
enough data. It supports `list_string`, positive configured maximum lengths for
scalar textual and date cells (including `text` and `edtf`), static allowed
values, Workbench link syntax for Drupal link fields, finite numeric syntax and
bounds, geolocation, configured authority sources, WebVTT media tracks, sealed
media-extension policies, taxonomy entity-reference bundles, and
typed-relation relators and bundles. Requiredness, cardinality, and reference
handler metadata remain part of the field contract.

A frozen `allowed_values_function` becomes `context_allowed_value`; Crosswalk
never executes the PHP callback name. Taxonomy `entity_reference` and modeled
`typed_relation` fields receive deterministic cell-grammar rules and, when an
entity type is known, a separate `context_entity_exists` rule. The standalone
CLI and offline transform do not query Drupal. A Drupal-configured live service
does query the fixed selected site for mapping-declared term IDs, names, and
URIs. When `allow_new_names` is true, only a plain taxonomy name that the live
resolver positively reports as missing may pass for later creation by the
Workbench task; numeric IDs and authority URIs must still resolve. Resolver
errors never become permission to create a term.

The shipped resolver supports only Drupal core's exact
`default:<entity_type>` reference handler, with `<entity_type>` equal to the
sealed query's entity type. `views`, `views:*`, and every other custom handler
fail closed with an unavailable-capability error until the selected site
provides a reviewed fixed adapter. Human labels never select handler behavior.

For a profile mapping encoded as `typed-identifier`, Crosswalk binds the source
field to the exact identity rule selected by the complete profile selector,
including an attribute or `attr0` discriminator. It copies that identity rule's
fully anchored expression into a mapping-declared `pattern` rule. During a
profile-bound check, Crosswalk first canonicalizes the cell with that exact
`profile_rule`, then applies the pattern to the canonical identifier value. It
does not choose a pattern from the human header.

For example, an AttrItemBase-style Drupal `textfield_attr` identifier field may
store DOI, accession, and other values in the same `field_identifier` storage.
The discriminator is part of the canonical source field and exact profile
selector:

```yaml
- name: field_identifier.attr0=accession
  label: Accession Number
  hub: Identifiers
  codec: profile_identifier
  profile_rule: example-accession
  source_type: textfield_attr
  validations:
    - rule: pattern
      pattern: '^EX-[0-9]{6}$'
```

Only the profile identity rule whose selector says `value where attr0 equals
accession` may supply that pattern. A DOI or another `attr0` sibling cannot
match this field by sharing the `field_identifier` base path. The pattern is a
bounded, fully anchored Go regular expression and is part of the sealed spec
fingerprint.

If several identity rules share one exact selector, they must declare the same
scheme, pattern, and canonicalizer or Workbench specification compilation
fails. Equivalent rules can still distinguish identity levels that the shared
Workbench cell cannot encode. Crosswalk selects the lexicographically first
rule name as the deterministic `profile_rule` representative, so the standard
equivalent `doi` and `doi-version` pair selects `doi` regardless of policy
order.

Table rules reference canonical names in `fields`:

| Rule | Field count | Meaning |
|---|---:|---|
| `unique` | 1 | Non-empty values are unique in the batch |
| `reference` | 2 | Values in the first field resolve against keys in the second |
| `preceding_reference` | 2 | Values in the first field resolve only against keys already seen in earlier rows of the second field |
| `different` | 2 | The two values must differ |
| `required_any` | 2 or more | At least one field is populated when the rule applies |
| `required_when` | 1 | The named field is populated when its predicate matches; `when.field` is the trigger and preferred diagnostic cell |

Every rule may restrict itself with `operations`. A `when` predicate names
another canonical field and uses one of these typed operators:

- `in` with a non-empty `values` list; or
- `value_count_greater_than` with a non-negative `count`.

The specification loader rejects unknown rules, phases, operators, field
references, duplicate operations, impossible parameter combinations, and
properties that do not belong to a rule. Validation criteria are included in
the specification fingerprint, so changing a rule makes an existing seal and
artifact contract stale.

## Example

This abbreviated source table makes all decisions in the mapping. The executor
does not contain special cases for the display labels `Title`, `Object Model`,
or `Supplemental File`.

```yaml
version: "1"
name: repository-items-workbench
source:
  format: csv
  header_rows: 1
  multi_value_separator: " ; "
  fields:
    - name: id
      label: Upload ID
      hub: Extra.id
      codec: unsigned
      required_for: [create]
      operations: [create]
    - name: parent_id
      label: Page/Item Parent ID
      hub: Extra.parent_id
      codec: unsigned
      operations: [create]
    - name: field_model
      label: Object Model
      hub: ObjectModel
      required_for: [create]
      operations: [create, update]
    - name: title
      label: Title
      hub: Title
      required_for: [create]
      operations: [create, update]
      validations:
        - rule: max_runes
          limit: 255
    - name: supplemental_file
      label: Supplemental File
      hub: Files.supplemental
      codec: file
      operations: [create, update]
      validations:
        - rule: media_extension
          media_types:
            - media_type: document
              select_extensions: [pdf]
              allowed_extensions: [pdf]
            - media_type: file
              allowed_extensions: [txt, zip]
              fallback: true
        - rule: context_file_readable
          phase: context
  validations:
    - rule: unique
      fields: [id]
      operations: [create]
    - rule: preceding_reference
      fields: [parent_id, id]
      operations: [create]
    - rule: required_when
      fields: [id]
      operations: [create]
      when:
        field: supplemental_file
        operator: value_count_greater_than
        count: 1
target:
  format: islandora-workbench
  header_rows: 1
  multi_value_separator: "|"
  fields:
    - name: id
      hub: Extra.id
      operations: [create]
    - name: parent_id
      hub: Extra.parent_id
      operations: [create]
    - name: field_model
      hub: ObjectModel
      operations: [create, update]
    - name: title
      hub: Title
      operations: [create, update]
    - name: supplemental_file
      hub: Files.supplemental
      codec: multi
      operations: [create, update]
```

Labels and aliases may change to match a local sheet. Rule references remain
`id`, `parent_id`, `field_model`, `title`, and `supplemental_file`; changing
presentation text alone cannot change validation behavior.

## Trust and execution

A sealed fingerprint proves which mapping was reviewed. It does not authenticate
who supplied a CSV, prove that Crosswalk generated an uploaded artifact, or
authorize repository mutation. Keep the sealed spec and sitectl contract in a
trusted deployment path, validate untrusted source data, and use explicit
context-aware preflight before operation. See
[Islandora Workbench](workbench.md#manifest-integrity-and-batch-authenticity).
