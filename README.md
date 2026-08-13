# crosswalk

A CLI tool for converting scholarly metadata between formats using a hub-and-spoke architecture.

## Install

### Homebrew

You can install Crosswalk using Homebrew:

```
brew tap lehigh-university-libraries/homebrew https://github.com/lehigh-university-libraries/homebrew
brew install lehigh-university-libraries/homebrew/crosswalk
```

### Download Binary

Instead of homebrew, you can download a binary for your system from [the latest release](https://github.com/lehigh-university-libraries/crosswalk/releases/latest)

Then put the binary in a directory that is in your `$PATH`

## Quick Start

```bash
# Install
go install github.com/lehigh-university-libraries/crosswalk@latest

# Convert Drupal JSON to CSV
crosswalk convert drupal csv -i export.json -o records.csv

# With an immutable site profile
crosswalk convert drupal csv -i export.json --source-profile my-site

# Create, review, seal, and publish a profile from Drupal config
crosswalk profile create drupal my-site \
  --config ./config/sync \
  --bundle islandora_object \
  --output my-site.draft.yaml
# Edit the ordered mappings and institution-specific identifier policy first.
crosswalk profile validate \
  --input my-site.draft.yaml \
  --output my-site.sealed.yaml
crosswalk profile publish --input my-site.sealed.yaml

# Compile a profile-bound, fingerprinted Workbench transformation
crosswalk spec compile drupal \
  --profile my-site \
  --output islandora-object-spec.draft.yaml
# Add context-host staging roots and local media-use term IDs, then reseal.
crosswalk spec validate \
  --input islandora-object-spec.draft.yaml \
  --output islandora-object-spec.yaml
# Create the independently provisioned trust anchor consumed by sitectl-isle.
crosswalk spec contract \
  --spec islandora-object-spec.yaml \
  --profile my-site \
  --output crosswalk-workbench-contract.json

# Apply that specification from the command line
crosswalk convert csv islandora-workbench \
  --spec islandora-object-spec.yaml \
  --target-profile my-site \
  --input metadata.csv \
  --output target.csv
```

`spec compile drupal --profile NAME` binds the exact published mapping and model
fingerprints. The model-only `--config`/`--bundle` form also accepts the
gzip-compressed tar archive produced by `sitectl-drupal` config export and is
useful before a profile is published. Compilation is local and deterministic:
it does not query Drupal or extract archive members. The result incorporates the
bundle's field storage types, cardinalities and settings; field labels,
descriptions and required policy; site provenance; Workbench operational
columns; and generic round-trip mappings for site-specific fields.

## HTTP Service

`crosswalk serve` replaces Fabricator's metadata HTTP service. Authentication
is mandatory. The simplest configuration uses a non-empty shared secret:

```bash
export SHARED_SECRET='replace-with-a-deployment-secret'
crosswalk serve
```

Google Apps Script can instead authenticate with the OpenID Connect token from
`ScriptApp.getIdentityToken()`, without configuring a shared secret on the
script. Crosswalk requires the token's exact audience and at least one identity
policy:

```bash
export CROSSWALK_GOOGLE_AUDIENCE='123456.apps.googleusercontent.com'
export CROSSWALK_GOOGLE_HOSTED_DOMAIN='example.edu'
# Optional exceptions or an alternative to the hosted-domain policy:
export CROSSWALK_GOOGLE_ALLOWED_EMAILS='person@example.edu,other@example.edu'
crosswalk serve
```

The equivalent flags are `--google-audience`, `--google-hosted-domain`, and
`--google-allowed-emails`; flag values take precedence over
`CROSSWALK_GOOGLE_AUDIENCE`, `CROSSWALK_GOOGLE_HOSTED_DOMAIN`, and
`CROSSWALK_GOOGLE_ALLOWED_EMAILS`. If both hosted-domain and email policies are
configured, a verified identity may match either. The shared-secret and Google
mechanisms can also be enabled together during migration. Startup fails if
neither is complete.

The Apps Script manifest must explicitly request `openid` and
`https://www.googleapis.com/auth/userinfo.email`. Send the returned token over
HTTPS as `Authorization: Bearer <token>`. The audience is the token's `aud`
claim—typically the script project's OAuth client ID—not the Crosswalk URL.
See Google's [Apps Script identity-token documentation](https://developers.google.com/apps-script/reference/script/script-app#getidentitytoken)
and [ID-token validation requirements](https://developers.google.com/identity/openid-connect/openid-connect#validatinganidtoken).

Crosswalk verifies Google's signature, issuer, configured audience,
expiration, optional not-before time, `email_verified`, and the configured
identity policy locally. Google's JWKS is fetched with a bounded client at
startup and cached with periodic refresh; tokens are not sent to Google's
`tokeninfo` endpoint.

To serve a site-specific compiled contract, export Drupal configuration with
`sitectl-drupal`, compile it, then start Crosswalk with the sealed spec:

```bash
sitectl job run drupal/config-export \
  --output /absolute/path/drupal-config.tar.gz
crosswalk spec compile drupal \
  --profile repository-items \
  --output islandora-object-spec.draft.yaml
# Add deployment paths/term IDs, then validate and reseal.
crosswalk spec validate \
  --input islandora-object-spec.draft.yaml \
  --output islandora-object-spec.yaml
crosswalk serve \
  --drupal-profile repository-items \
  --spec islandora-object-spec.yaml
```

Specification loading is strict. Unknown spec keys, invalid mappings, and a
fingerprint that no longer matches the policy all prevent conversion or server
startup. With `--drupal-profile`, `serve` derives its default transformation
from that profile's exact stored model and mappings and rejects an explicit
`--spec` bound to any other profile or model. Without either option, it uses the
built-in Fabricator spreadsheet compatibility contract.

The service exposes:

| Method | Path | Request | Response |
|--------|------|---------|----------|
| `GET` | `/healthcheck` | none | `text/plain` |
| `POST` | `/workbench/check` | JSON array of spreadsheet rows | JSON cell-to-error map |
| `POST` | `/workbench/transform` | CSV spreadsheet export | ZIP of Workbench CSV artifacts |
| `POST` | `/workbench/matches?mode=hold` | CSV spreadsheet export | ZIP of JSON report and review CSV |

All Workbench routes require either a valid bearer token or the secret in
`X-Secret`:

```bash
curl --fail --silent \
  --header "X-Secret: $SHARED_SECRET" \
  --header 'Content-Type: application/json' \
  --data-binary @csv.json \
  http://localhost:8080/workbench/check

curl --fail --silent \
  --header "X-Secret: $SHARED_SECRET" \
  --header 'Content-Type: text/csv' \
  --data-binary @source.csv \
  --output files.zip \
  http://localhost:8080/workbench/transform
```

Transformation is side-effect-free: it does not contact or mutate Drupal,
read referenced media files, or use shared temporary files. The separate
matches route may perform configured read-only Drupal queries. Each request builds
its deterministic artifact bundle in memory. Request sizes and HTTP server
timeouts are bounded by `--max-body-bytes`, `--max-output-bytes`, `--max-rows`,
`--max-cells`, `--max-concurrent-requests`, `--max-header-bytes`,
`--request-timeout`, `--read-header-timeout`, `--read-timeout`,
`--write-timeout`, and `--idle-timeout`; run `crosswalk serve --help` for
defaults. The default listener is loopback-only. A non-loopback plaintext bind
is rejected unless `--allow-public-http` is explicit; use that flag only behind
a trusted TLS-terminating proxy.

Configure `serve --drupal-jsonapi URL --drupal-profile NAME` with the named
JSON:API route resolved by sitectl-drupal and a stored profile compiled from
that site's Drupal model. The profile supplies the repository entity/bundle,
lookup fields, identifier authorities, and exact-identity policy; Crosswalk
does not guess them from a default Islandora installation. A live Drupal lookup
without a valid Drupal profile fails before the server starts. Credentials may
come from `DRUPAL_JSONAPI_TOKEN`, or the username/password environment variables
shown by `serve --help`. Strong profile-approved identifiers are queried first;
title/author/date candidates are always held for review. Modes are `hold`
(default), `skip`, `force-new`, and `assume-new`; the last skips repository
queries but still detects duplicates inside the batch. JSON reports and review
CSVs record the identifier-registry digest and, when configured, the profile
and model fingerprints used for the decision.

## Instance-specific profiles

Profiles are immutable, fingerprinted executable mappings bound to an exact
source-system model. Profile authoring is intentionally a three-step workflow:
`create` writes a heuristic draft, `validate` checks edited selectors, codecs,
merge policies, cardinalities, identifier authorities, and model compatibility
before sealing a new fingerprint, and `publish` atomically installs that sealed
definition. Creation stores the content-addressed model but never silently
publishes guessed mappings.

For Drupal, sitectl owns active configuration acquisition and Crosswalk owns
the resulting model and mapping policy:

```bash
sitectl drupal crosswalk profile create repository-items \
  --bundle islandora_object \
  --config-dir "$PWD/.crosswalk" \
  --output repository-items.draft.yaml
crosswalk profile validate --config-dir "$PWD/.crosswalk" \
  --input repository-items.draft.yaml \
  --output repository-items.sealed.yaml
crosswalk profile publish --config-dir "$PWD/.crosswalk" \
  --input repository-items.sealed.yaml
```

Omeka S uses the same model-bound workflow. The supplied JSON is a bounded
schema acquisition snapshot containing vocabularies, properties, resource
classes, and resource templates; Crosswalk does not query the Omeka API or its
database while authoring a profile:

```bash
crosswalk profile create omeka-s photographs \
  --snapshot omeka-schema.json \
  --resource-template-id 200 \
  --output photographs.draft.yaml
crosswalk profile validate \
  --input photographs.draft.yaml \
  --output photographs.sealed.yaml
crosswalk profile publish --input photographs.sealed.yaml
```

An institution-owned identifier becomes exact duplicate evidence only when its
profile explicitly supplies a distinct scheme, absolute authority namespace,
anchored validation pattern, and identity level. Generic local identifier
fields are never assumed globally unique. ArchivesSpace transformations use its
versioned JSONModel API shape rather than an instance profile; hierarchy is
carried through the Dataset model into Workbench `id`, `parent_id`, and sibling
weight columns. ArchivesSpace JSONModel evolves independently of Crosswalk, so
strict parsing validates supported record types and the structure of official
fields but accepts newer or plugin-defined top-level fields. Unrecognized
values are retained in `Extra.archivesspace_raw_fields` as canonical JSON text,
which preserves JSON types and integer precision for later profile-aware use.

Source-acquisition commands (`fetch doi`, `fetch arxiv`, `fetch crossref`,
`fetch wos`, `fetch scopus`, `fetch zenodo`, and `fetch proquest`) default to
`--existing hold` and therefore require both `--drupal-jsonapi` and
`--drupal-profile`. This makes repository duplicate checks the safe default for
batch discovery jobs. A known-new ingest must opt out explicitly with
`--existing assume-new`; Crosswalk still catches duplicates within that input
batch. Supplying `--drupal-profile` in assume-new mode applies the same
institution-specific identity policy to those in-batch checks. Use
`--match-report` and `--match-review` to persist the JSON decision record and CSV
metadata-difference queue before ingest. Every acquisition command has an
explicit record bound; `fetch doi` accepts at most `--max-records` unique DOI
arguments/file entries (1000 by default).

Duplicate reconciliation happens before optional SHERPA enrichment, PDF
downloads, or ProQuest media publication, so held and skipped items do not
leave acquisition files behind. When a Workbench fetch uses `--media-dir`, a
relative value is resolved beneath the selected transformation contract's
staging root before the file is written. The path in the resulting Workbench
CSV therefore names the file that was actually staged. Fetch uses the profile's
exact Drupal model and mappings when `--drupal-profile` is supplied and rejects
a `--spec` from another profile or model. A profile-derived spec deliberately
has no host paths or local term IDs; media workflows must pass a reviewed, resealed `--spec`
containing that deployment policy. Only profile-less compatibility workflows
fall back to the built-in contract.

After review, `crosswalk spec contract --spec SPEC --profile PROFILE` writes the
artifact-free JSON trust anchor for sitectl-isle. It rejects a spec compiled
from a different profile or model. Provision that file as site configuration
outside every uploaded artifact directory; never create it by copying a batch manifest.

The built-in contract declares `file.staging_root: /mnt/islandora_staging` and
`file.allowed_absolute_roots: /home|/mnt` in its `defaults` map. Relative
primary and supplemental file paths are cleaned and written beneath the
staging root, including paths that use Windows-style separators. Absolute
paths under `/home` or `/mnt` remain absolute; other leading-slash paths are
treated as staging-relative for compatibility (for example, `/etc/passwd`
becomes `/mnt/islandora_staging/etc/passwd`). Relative paths that would escape
the staging root, UNC paths, Windows drive paths, and URLs are rejected. Drupal-
compiled specs intentionally leave these deployment policy keys unset:
`config/sync` describes Drupal's data model, not the context host's mounts or
local taxonomy term IDs. Reviewers must add institution-specific clean absolute
POSIX roots and supplemental-media pairs, then run `crosswalk spec validate` to
validate and reseal the edited spec before those workflows are used.

Workbench accepts only one file in an `additional_files` cell. When a source
row has multiple published supplemental files, the first remains in that
row's `supplemental_file` column. For an existing node, every remaining file
becomes its own row in `target.add_media.csv`. For a new node, remaining files
are written one per row to `target.pending_supplemental.csv`; sitectl resolves
its upload `id` to the created `node_id` before running the add-media task.
The media-use and publication values come from the spec defaults
`supplemental.media_use_tid` and `supplemental.published`. The built-in contract
keeps Fabricator's media-use term ID `151326` and published value `1`.

Every Workbench artifact plan includes `crosswalk-artifacts.json`. The
file is written automatically to `fetch --artifact-dir` output and included in
the ZIP returned by the HTTP transform endpoint. Manifest version 1 has this
shape (digests and byte counts are illustrative):

```json
{
  "version": 1,
  "spec": {
    "name": "fabricator-workbench",
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

`csv_rows` counts data rows and excludes the header. The manifest lists every
other artifact in plan order and never lists or digests itself. Target profile
and model fingerprints are emitted only as a pair when the conversion uses an
immutable target system profile. Policy keys are emitted only when they are
known from the validated transformation specification; Crosswalk does not
invent staging roots or supplemental-media defaults for custom specifications.
Consumers should reject unsupported manifest versions, fingerprint or digest
mismatches, unexpected files, and missing policy required by the operation
they intend to run.

The check endpoint reports line breaks in taxonomy and multi-value cells while
allowing multiline prose fields. A resource type remains required for ordinary
create rows when that spreadsheet column is present, but is optional for Page
and Sub-Collection object models.

## How It Works

```
Source Format    Profile           Hub Record         Rules            Target Format
─────────────    ───────           ──────────         ─────            ─────────────
Drupal JSON  →   field mappings →  Record      →      type mapping  →  schema.org
CSV          →   column mappings → (normalized) →     field rules   →  CrossRef XML
```

- **Profiles** define immutable, model-bound source mappings and identity policy (`~/.crosswalk/profiles/`)
- **Models** capture source-system schemas by content fingerprint (`~/.crosswalk/models/`)
- **Specs** define direction-aware tabular transformations and operational artifact policy
- **Hub schema** is defined in Protocol Buffers (`hub/v1/hub.proto`)

## Supported Formats

| Format              | Parse | Serialize |
|---------------------|-------|-----------|
| Drupal JSON         | ✓     | ✓         |
| CSV                 | ✓     | ✓         |
| schema.org JSON-LD  | ✓     | ✓         |
| CrossRef XML        | ✓     | ✓         |
| DataCite XML        | ✓     | ✓         |
| ProQuest ETD        | ✓     | ✓         |
| BibTeX              | ✓     | ✓         |
| CSL-JSON            | ✓     | ✓         |
| MODS XML            | ✓     | ✓         |
| Dublin Core         | ✓     | ✓         |
| arXiv               | ✓     | ✓         |
| Islandora Workbench | ✓     | ✓         |
| Web of Science JSON | ✓     | —         |
| Crossref REST JSON  | ✓     | —         |
| Scopus JSON         | ✓     | —         |
| Zenodo JSON         | ✓     | —         |
| Omeka S JSON-LD     | ✓     | —         |
| ArchivesSpace JSONModel | ✓ | —         |

Have an idea for a new format? Issues and Pull Requests welcome!
