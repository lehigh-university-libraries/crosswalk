# Existing-item reconciliation

Crosswalk detects likely duplicate records before output generation without
changing the repository. Candidate retrieval is a read-only concern; comparison
and partitioning are deterministic and separately reportable.

## Safe default for acquisition

All source-acquisition commands default to `--existing hold`:

- `fetch doi`
- `fetch arxiv`
- `fetch crossref`
- `fetch wos`
- `fetch scopus`
- `fetch zenodo`
- `fetch proquest`

In `hold`, `skip`, and `force-new` modes, repository lookup requires both the
Drupal JSON:API route and a published Drupal profile:

```bash
crosswalk fetch crossref \
  --query 'repository metadata' \
  --max-records 100 \
  --drupal-jsonapi https://repository.example.edu/jsonapi \
  --drupal-profile repository-items \
  --match-report matches.json \
  --match-review matches.csv \
  --to csv \
  --output accepted.csv
```

The endpoint should be the named JSON:API route resolved by sitectl-drupal. The
profile supplies the repository entity/bundle, modeled lookup fields,
identifier authority, and exact-identity policy. Crosswalk does not infer those
properties from a default Islandora installation.

Credentials may be supplied through the environment variables named by
`--drupal-token-env`, or by the username/password environment flags shown by
`crosswalk fetch --help`. Configure bearer or Basic authentication, not both.

## Identifier-first matching

Reconciliation evaluates evidence in this order:

1. Canonicalize incoming identifiers through the effective profile registry.
2. Query only identifiers that the profile marks strong exact evidence for
   their scheme, authority namespace, and identity level.
3. Compare every returned candidate in the Hub, not merely its search hit.
4. If no exact candidate resolves and a title is available, run the profile's
   ordered, bounded metadata lookup strategies.
5. Compare title, contributors, and date and record field-level differences.

Metadata-only evidence never becomes an automatic duplicate under policy
version 1. One metadata candidate produces `review`; several produce
`ambiguous`. Several exact identifier candidates are also ambiguous. Even one
exact identifier match can require review when the identity level does not
permit an automatic duplicate decision.

Each incoming record is also compared with earlier records in the same batch.
That check remains active in `assume-new` mode.

## Modes

| Mode | Repository query | Partition behavior |
|---|---|---|
| `hold` | Yes | Accept new records; hold duplicate, review, and ambiguous records for manual resolution |
| `skip` | Yes | Accept new records, skip confirmed duplicates, and hold review/ambiguous records |
| `force-new` | Yes | Produce a report, then accept every verdict as new output |
| `assume-new` | No | Accept repository-unknown records but still hold duplicate/review/ambiguous records found inside the batch |

Use `assume-new` only when the repository lookup is intentionally unnecessary,
such as a Workbench batch known to contain entirely new records. Supplying
`--drupal-profile` in this mode still applies its institution-specific
identifier registry to in-batch comparisons.

`force-new` is different: it performs the configured repository search and
records what it found, but deliberately accepts all results. It should be an
explicit, reviewed choice.

## Reports and manual review

Use both report outputs for batch work:

```bash
--match-report matches.json \
--match-review matches.csv
```

The JSON report captures versioned decisions, candidates, evidence, and
metadata differences. The CSV is safe to open in spreadsheet software and is
designed as the manual queue for deciding whether to update an existing item,
discard incoming metadata, or create a separate item. Both record the effective
identifier-registry digest and, when a site profile is configured, its system,
profile name, profile fingerprint, and model fingerprint.

If a report requires review and no durable CSV path was supplied, Crosswalk
writes the review CSV to standard error and stops before serialization. In
`skip` mode, confirmed duplicates are omitted and Crosswalk warns that their
incoming metadata still needs an explicit reconciliation decision.

## Crossref discovery flow

`fetch crossref` uses the Crossref REST API and the parse-only `crossref-rest`
adapter. It does not parse the response as Crossref deposit XML. The complete
flow is:

```mermaid
flowchart LR
  Search[Crossref REST search] --> REST[crossref-rest JSON parser]
  REST --> Hub[hubv1.Record]
  Hub --> Existing[identifier-first reconciliation]
  Existing --> Target[target serializer]
```

The target serializer is selected with `--to`; choosing `crossref` writes
Crossref deposit XML, while choosing `islandora-workbench` writes a reviewed
Workbench artifact plan. See [Architecture](architecture.md#crossref-has-two-distinct-adapters)
for the format boundary.

## Acquisition and files

Every acquisition command has explicit page and record bounds. `fetch doi`
accepts at most `--max-records` unique DOI arguments/file entries (1000 by
default); API-backed searches have source-specific page-size, page-count, and
record limits visible in their help.

Duplicate reconciliation happens before optional SHERPA enrichment, PDF
downloads, or ProQuest media publication. Held or skipped items therefore do
not leave acquisition files behind. When a Workbench fetch uses `--media-dir`,
a relative path is resolved beneath the sealed spec's staging root before the
file is written, and the emitted CSV names the file actually staged.
