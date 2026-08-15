# Islandora Workbench

Crosswalk turns Hub records into deterministic Islandora Workbench CSV
artifacts. A sealed transformation specification controls the source table,
target fields, operation applicability, validation rules, path handling, and
local media policy. Sitectl-isle owns its documented context-aware operations:
media preflight, supplemental reconciliation, guarded media retry, and rollback.
Neither project currently provides a general create/update bundle executor.

## Compile a site contract

Compile a draft from a published Drupal profile:

```bash
crosswalk spec compile drupal \
  --profile repository-items \
  --output islandora-object-spec.draft.yaml
```

This binds the exact published profile and model fingerprints. The generated
fields come from the profile's executable mappings; Crosswalk does not guess a
second Drupal mapping at serialization time.

Before a profile is published, the model-only form can compile directly from a
`config/sync` directory or sitectl-drupal gzip tar export:

```bash
crosswalk spec compile drupal \
  --config /absolute/path/drupal-config.tar.gz \
  --bundle islandora_object \
  --output islandora-object-spec.draft.yaml
```

Compilation is offline and deterministic. It does not query Drupal and does
not extract archive members onto disk.

By default, compilation seals taxonomy references as strict live existence
checks. Add `--allow-new-taxonomy-terms` to either form of `spec compile
drupal` when the later Workbench task is allowed to create missing plain term
names:

```bash
crosswalk spec compile drupal \
  --profile repository-items \
  --allow-new-taxonomy-terms \
  --output islandora-object-spec.draft.yaml
```

This option records `allow_new_names` in the affected taxonomy context rules;
it does not create a term or make compilation contact Drupal. Numeric term IDs
and authority URIs are never covered and must resolve in a live check.

## Review and seal

A Drupal model describes fields; it does not describe the context host's
mounts or the site's local taxonomy term IDs. A profile-derived spec therefore
leaves deployment defaults unset. Review the draft and add the policy required
by the intended jobs, including as applicable:

```yaml
defaults:
  file.staging_root: /mnt/islandora_staging
  file.allowed_absolute_roots: /home|/mnt
  supplemental.media_use_tid: "151326"
  supplemental.published: "1"
  unpublished_supplemental.media_use_tid: "151326"
  unpublished_supplemental.published: "0"
```

The numeric values above are the legacy compatibility defaults, not universal
Islandora values. Replace them with the reviewed terms and publication policy
for the target site.

Seal the edited draft:

```bash
crosswalk spec validate \
  --input islandora-object-spec.draft.yaml \
  --output islandora-object-spec.yaml
```

Specification decoding is strict. Unknown keys, unsupported mappings,
incomplete policy pairs, invalid paths, and stale fingerprints fail validation.
Editing a sealed spec makes its fingerprint stale; run `spec validate` again
before using it. See [Transformation specifications](specifications.md) for the
complete field, table, operation, and validation-rule schema.

## Check a downloaded Google Sheet

Download the relevant Google Sheet tab as CSV and pass that file directly to
Crosswalk. JSON is not an intermediate format:

```bash
crosswalk workbench validate \
  --input metadata.csv \
  --spec islandora-object-spec.yaml
```

For a published, model-bound Drupal profile, supply the exact stored profile as
well:

```bash
crosswalk workbench validate \
  --input metadata.csv \
  --spec islandora-object-spec.yaml \
  --drupal-profile repository-items
```

This is the replacement for Fabricator's **Check My Work** path. It is not a
wrapper around Workbench `--check`. Crosswalk resolves every source header
through the sealed spec, then applies canonical field rules compiled from the
frozen Drupal model plus workflow rules declared in the mapping. Changing a
human label or adding an alias does not change which validation runs.

The built-in Fabricator mapping requires `Resource Type` on create rows except
when the exact object-model value is `Page` or `Sub-Collection`. That exception
is sealed as `optional_for_object_models`; it is not inferred from either
field's display label.

Drupal compilation currently carries requiredness, separator-aware cardinality,
configured scalar text/date lengths (including `text` and EDTF), `list_string`
and other static allowed values, Workbench `URL%%label` links, finite numbers
and bounds, geolocation, configured authority sources, WebVTT media tracks,
entity-reference bundles, typed-relation relators, and field/reference handler
metadata into the sealed contract. Exact profile-identifier mappings also carry
their anchored identity pattern; a bound check canonicalizes the identifier
through that same profile rule before matching it. Spreadsheet workflow rules
cover required and unique upload IDs, parents that must reference an earlier
create row, operation applicability, contributor shape, EDTF syntax, rights
values, and line breaks.

Media validation is also sealed rather than selected from an object-model label
or another sheet field. Each primary, published supplemental, and unpublished
supplemental path carries `media_types` entries with filename
`select_extensions`, model-derived `allowed_extensions`, and exactly one
fallback media type. Profile compilation reads extensions only from the exact
standard Workbench file field for each standard media bundle; it neither
substitutes another file-valued field nor automatically emits a custom bundle.
Custom bundles and selectors require an explicit complete `media_types` policy
in the reviewed, sealed specification. Directly mapped Drupal `file`, `image`,
and `media_track` fields that declare `file_extensions` still carry the
extension allowlist from that exact modeled field. See
[Transformation specifications](specifications.md#validation-rules) for the
policy shape.

For profile-derived Workbench base fields, `created` must use the exact
timezone-bearing timestamp form and cannot be in the future; `langcode` must be
one of the sealed Workbench-supported Drupal language codes; and every non-empty
`url_alias` must begin with `/` and be unique within the input sheet. These
decisions use canonical mapping fields, never display labels. The command
reports cell-addressed JSON and exits nonzero on findings.

Rules marked `context` need the selected site or staging filesystem. The
Crosswalk engine can execute them only when its caller installs a trusted
context resolver. The `workbench` CLI does not install one, so a plain local
invocation remains deterministic and does not silently contact Drupal, Getty,
or the host filesystem. `crosswalk serve` does install Crosswalk's read-only
Drupal, local-file, and Getty resolvers when both `--drupal-jsonapi` and the
exact `--drupal-profile` are configured. It queries the fixed selected site for
mapping-declared node and entity references, including taxonomy term IDs,
names, and URIs. Dynamic Drupal `allowed_values_function` providers and
entity-reference existence become typed context queries containing sealed model
constraints and inert cell data; the resolver never executes a provider name
locally or follows a spreadsheet value as a destination.

For a profile-derived service contract, `crosswalk serve` defaults
`--workbench-allow-new-taxonomy-terms` to true for Fabricator parity. Set
`--workbench-allow-new-taxonomy-terms=false` to require every taxonomy value to
exist. Even in permissive mode, only a plain name positively reported missing
may pass for creation by the later Workbench task; numeric IDs and authority
URIs must resolve, and lookup errors fail the check. An explicit sealed `--spec`
retains its own `allow_new_names` policy rather than being rewritten at startup.

Dynamic callback checks fail explicitly until the selected site provides a
fixed adapter. Entity-reference checks support only Drupal core's exact
`default:<entity_type>` handler for the sealed entity type; `views`, `views:*`,
and every other custom handler fail closed pending their own fixed adapter.
See [HTTP service](http-service.md#live-check-my-work-context) for the live
configuration and staging-host requirement. Sitectl-isle's separate
`workbench-preflight` operation checks filesystem access as the effective
selected-context user.

This validation replaces Fabricator's **Check My Work** metadata service and
the data-model-driven portion of Workbench `--check`; it does not claim every
operational check performed by the Workbench program. Crosswalk does not parse
a Workbench task YAML or verify its Drupal credentials, Integration module
version, input and rollback destination writability, hook executables, row
filters/templates, remote-media accessibility, checksums, OCR or media-track
file contents/encoding, media-use or derivative conflicts, or task-specific
configuration. The offline command validates URL-alias shape and within-sheet
uniqueness; the configured live service additionally queries only its fixed
selected Drupal origin to reject an alias already in use. Keep the remaining
deployment and execution checks in the authorized Workbench runner until
equivalent sealed rules or selected-context operations exist.

Once validation is clean, write the complete operation bundle atomically:

```bash
crosswalk workbench transform \
  --input metadata.csv \
  --spec islandora-object-spec.yaml \
  --artifact-dir ./workbench-batch
```

The destination must not already exist. Validation happens before publication,
so an invalid sheet leaves no partial artifact directory. Every create row must
have an `Upload ID`; a `Page/Item Parent ID`, when present, must name an upload
ID from an earlier create row. A create sheet routes to `target.csv`; a metadata
sheet with `Node ID` routes to
`target.update.csv`; and a row containing only `Node ID` plus primary `File
Path` routes to `target.add_media.csv`. A row that mixes update metadata with a
primary file is rejected instead of silently dropping either payload.

Library callers and non-CSV adapters may supply Hub records without the
parser's `_source_columns` provenance. For those records, artifact planning
projects every active update target through the same value path used by the
serializer instead of consulting a partial list of Hub fields. A record with a
node ID, primary file, and `Publisher`, for example, routes to
`target.update.csv` and retains the publisher value rather than being mistaken
for an add-media-only record.

For explicit create and update fixture-shaped inputs:

```bash
# Blank Node ID plus create metadata -> create-workbench/target.csv
crosswalk workbench transform \
  --input create.csv \
  --spec islandora-object-spec.yaml \
  --artifact-dir ./create-workbench

# Node ID plus metadata and no primary file -> update-workbench/target.update.csv
crosswalk workbench transform \
  --input update.csv \
  --spec islandora-object-spec.yaml \
  --artifact-dir ./update-workbench
```

These commands generate task inputs; they do not create or update a Drupal
node. An explicitly authorized Workbench runner owns that mutation step.

## ProQuest ETD XML

`go-islandora` can turn a standalone ProQuest XML delivery into the same
human-readable CSV contract. The media root is explicit because a standalone
XML file does not supply the ZIP directory context:

```bash
go-islandora transform etd \
  --source etd.xml \
  --target etd.csv \
  --media-root /mnt/islandora_staging/etds

crosswalk workbench validate --input etd.csv --spec islandora-object-spec.yaml
crosswalk workbench transform \
  --input etd.csv \
  --spec islandora-object-spec.yaml \
  --artifact-dir ./etd-workbench
```

Fresh ETD CSV rows intentionally have a blank `Node ID`, so they produce a
create artifact. A post-ingest sheet containing `Node ID`, metadata, and a
primary file is ambiguous and is rejected; split it into an update sheet and an
add-media sheet if both operations are intended.

The repositories include redacted ETD/create/update fixtures and complete
byte-for-byte Crosswalk artifact goldens. A separate synthetic Fabricator
reference verifies the shared canonical columns. Fabricator's live contributor
resolution is not used as an offline oracle because that legacy transform may
create Drupal taxonomy terms.

Run the fixture contracts from the corresponding repository roots:

```bash
# crosswalk: direct CSV validation, operation routing, complete artifact goldens,
# label-independent rules, and the synthetic Fabricator field reference
go test ./cmd -run '^TestWorkbench'

# go-islandora: standalone redacted XML and retained ZIP-directory behavior
go test ./cmd -run '^TestTransformETD|^TestETD|^TestBuildETD'

# fabricator: deterministic synthetic reference CSV
go test ./internal/handlers -run '^TestTransformCsvMatchesDeterministicGolden$'
```

The Crosswalk fixtures are in `cmd/testdata/workbench/`; their `golden/`
subdirectories contain the exact expected CSVs and manifests. The ETD XML is
redacted and synthetic. Refresh a golden only after reviewing the semantic
change—do not derive an expected file from the same code path during the test.

## Migrating the Fabricator runner

The previous GitHub runner mixed acquisition, metadata checks, transformation,
site-context checks, mutation, and notifications in one job. Keep those stages
explicit in replacement automation:

| Runner responsibility | Replacement boundary |
|---|---|
| Validate a Google sharing URL and range, authenticate to Sheets, and download a tab | A trusted fixed-origin Sheets client; save its rectangular result as CSV |
| **Check My Work** metadata validation | `crosswalk workbench validate`, or authenticated `/workbench/check` for a JSON string table |
| Derive field limits, cell grammar, static allowed values, reference bundles, and exact profile identifier patterns | A reviewed spec compiled from the selected site's immutable Drupal model/config snapshot and profile |
| Check node/entity existence, staged files, or Getty TGN facts | `crosswalk serve` with the exact profile and selected fixed Drupal endpoint; use sitectl-isle media preflight when the service is not on the staging host |
| Check taxonomy references | A live service queries the fixed selected site; optionally allow only missing plain names for later Workbench creation, while IDs and URIs must resolve |
| Check a dynamic Drupal `allowed_values_function` or a non-`default:<entity_type>` entity-reference handler | Fails explicitly until the selected site provides a reviewed fixed adapter; this includes `views`, `views:*`, and every other custom handler, and Crosswalk never executes a model callback name or spreadsheet URI locally |
| Produce create, update, add-media, agent, and supplemental artifacts | `crosswalk workbench transform` or authenticated `/workbench/transform` |
| Require at least one output row and operation-specific headers | Crosswalk rejects an empty transform, derives headers from the sealed target table, routes the operation-specific filename, and records parsed CSV row counts in the manifest; validate that manifest instead of using physical line counts or substring header searches |
| Run ordinary Workbench create/update/add-media tasks | An explicitly authorized external Workbench runner; no general sitectl job currently claims this step |
| Write node IDs back to a Sheet | External Google orchestration after retaining and verifying the exact Workbench result |
| Scheduling, concurrency, credentials, logs, and Slack messages | Deployment orchestration with separate least-privilege credentials |

Do not treat URL/range checking as metadata validation, or a clean
deterministic result as proof that files and referenced entities exist in the
selected deployment. The same acquired grid must feed validation and
transformation so a second normalization path cannot change the reviewed data.

## Create the sitectl trust anchor

Create an artifact-free contract from the reviewed sealed spec and exact
published profile:

```bash
crosswalk spec contract \
  --spec islandora-object-spec.yaml \
  --profile repository-items \
  --output crosswalk-workbench-contract.json
```

Provision this file as trusted site configuration outside every uploaded batch
directory. Do not derive it by copying a batch manifest: the point is to compare
an untrusted batch with independently reviewed configuration. Contract creation
rejects a profile or model fingerprint that differs from the spec.

## Convert records

Use the same profile at the target boundary:

```bash
crosswalk convert csv islandora-workbench \
  --spec islandora-object-spec.yaml \
  --target-profile repository-items \
  --input metadata.csv \
  --output target.csv
```

Profile-bound specs fail closed when the runtime profile is missing or has a
different profile/model fingerprint. An unbound spec cannot be combined with a
target profile.

Acquisition commands can write the complete named artifact plan atomically:

```bash
crosswalk fetch proquest delivery.zip \
  --existing assume-new \
  --spec islandora-object-spec.yaml \
  --drupal-profile repository-items \
  --artifact-dir ./workbench-batch
```

`--artifact-dir` is available only with `--to islandora-workbench` and cannot be
combined with `--output`.

## File path policy

For a sealed spec with `file.staging_root` and
`file.allowed_absolute_roots`:

- relative primary and supplemental paths are cleaned and rooted below the
  staging root;
- Windows-style separators in relative paths are normalized;
- allowlisted clean POSIX absolute paths stay absolute;
- other leading-slash paths are treated as staging-relative for compatibility;
- paths that escape the staging root, UNC paths, Windows drive paths, URLs,
  control characters, and a filesystem-root staging policy are rejected.

For example, under the compatibility policy `/etc/passwd` is emitted as
`/mnt/islandora_staging/etc/passwd`; it is not authorization to read the host
path. Transformation is side-effect-free and does not read referenced media.
When `fetch --media-dir` performs an authorized download, it stages the file
first and emits that actual path.

## Artifact planning

The planner groups records by Workbench operation and may emit:

| Artifact | Purpose |
|---|---|
| `target.csv` | Create rows |
| `target.update.csv` | Node update rows |
| `target.add_media.csv` | Existing-node media rows |
| `agents.csv` | Unique contributor taxonomy rows |
| `target.pending_supplemental.csv` | Additional published media for a newly created upload ID |
| `target.unpublished_supplemental.csv` | Unpublished supplemental media rows |
| `crosswalk-artifacts.json` | Integrity manifest for every other emitted artifact |

Workbench accepts only one path in an `additional_files` cell. The first
published supplemental file stays on its parent record. Each remaining file
for an existing node becomes a separate add-media row. For a new node, each
remaining file is written to `target.pending_supplemental.csv`; sitectl resolves
the upload `id` to the created `node_id` before running the add-media job.

`agents.csv` is a deterministic review artifact containing unique contributor
taxonomy rows and extended person metadata. Offline validation and
transformation do not query, resolve, or create those rows, and sitectl-isle
does not currently expose an agents execution job. A separately configured live
Check My Work service may query mapping-declared terms for existence, but it
still never creates them. Treat term creation as a separately reviewed site
operation rather than an implicit part of transformation.

Positional supplemental reconciliation assumes the rollback node IDs are in
exactly the same data-row order as the `target.csv` passed to that Workbench
create run. Retain both files from the same run and do not sort, filter, append,
or reuse either one before reconciliation. Sitectl rejects a row-count mismatch,
but equal counts alone cannot detect a reordered rollback artifact.

## Operation and context boundary

Crosswalk parses, validates deterministic mapping rules, and plans artifacts; it
does not run Islandora Workbench or mutate Drupal. Validation rules that require
the selected site, filesystem, or an authority resolver are marked `context` in
the specification. A Drupal-configured `crosswalk serve` executes the shipped
read-only node/entity, local-file, and Getty resolvers. The service process must
run on a host with the exact staging mount; resolving a remote Drupal URL does
not expose its filesystem. Otherwise, run sitectl-isle's media preflight, which
verifies allowed roots, symlink boundaries, regular files, and effective-user
readability inside the selected context.

The current sitectl-isle jobs do not constitute a general plan executor. Create
and update CSVs still require an explicitly managed Workbench run. The guarded
retry and rollback jobs validate exact input, restricted Workbench
configuration, selected-context host, and expected success log entries. Their
log parsers are coupled to the Workbench release in production; confirm the
supported messages before upgrading Workbench.

## Manifest

Every artifact plan includes manifest version 1. The manifest identifies the
exact spec, includes target profile/model fingerprints only as a pair, records
known operational policy, and lists each other artifact in deterministic order:

```json
{
  "version": 1,
  "spec": {
    "name": "repository-items-workbench",
    "version": "1",
    "fingerprint": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  },
  "profile_fingerprint": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
  "model_fingerprint": "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
  "policy": {
    "path_mode": "staged-posix",
    "staging_root": "/mnt/islandora_staging",
    "allowed_absolute_roots": ["/home", "/mnt"],
    "supplemental_media_use_tid": "151326",
    "pending_supplemental_published": "1",
    "unpublished_supplemental_media_use_tid": "151326",
    "unpublished_supplemental_published": "0"
  },
  "artifacts": [
    {
      "path": "target.csv",
      "media_type": "text/csv; charset=utf-8",
      "sha256": "23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01",
      "bytes": 4096,
      "csv_rows": 12
    }
  ]
}
```

`csv_rows` excludes the header. The manifest never lists or digests itself.
Policy keys appear only when known from the spec; Crosswalk does not invent
deployment values for custom configurations. Consumers should reject unknown
manifest versions, fingerprint or digest mismatches, unexpected or missing
files, and missing policy required by the intended operation.

## Manifest integrity and batch authenticity

The independently provisioned contract pins the reviewed spec, profile/model
provenance, and stable operational policy. It prevents an uploaded manifest from
substituting another mapping or site policy. The batch manifest then records
each artifact's digest, byte count, media type, and row count.

This split provides schema/policy trust and useful integrity checks, but version
1 does not sign or authenticate the batch. A party that can replace both an
artifact and its uploaded manifest can recompute the per-batch digest while
copying the contract's public spec fingerprint. Therefore:

- provision the contract independently from every batch;
- deliver batches through an authenticated, access-controlled path;
- retain or attest the original manifest at the production boundary;
- re-run deterministic validation and required context preflight before a
  general mutation; and
- do not describe a matching digest as proof that Crosswalk generated or
  approved the data.

A future signing or attestation layer may authenticate batch origin. Until then,
manifest validation must be combined with trusted transport and explicit
operator review.

## Compatibility contract

When a profile-less fetch or HTTP service does not receive an explicit spec,
Crosswalk can use its sealed Fabricator-compatible spreadsheet contract. That
contract retains the legacy column layout, staging roots `/home|/mnt`, media-use
term `151326`, and publication values `1`/`0`. New installations should prefer a
profile-derived, locally reviewed spec rather than relying on those historical
defaults. See [Boundaries, migration, and integration status](limitations.md)
for features intentionally not reproduced from the retired workflows.
