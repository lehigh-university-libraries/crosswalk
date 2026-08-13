# Islandora Workbench

Crosswalk turns Hub records into deterministic Islandora Workbench CSV
artifacts. A sealed transformation specification controls the source table,
target fields, operation applicability, path handling, and local media policy.
Sitectl-isle owns validation against the independently provisioned contract and
execution of the Workbench jobs.

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
before using it.

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

## Compatibility contract

When a profile-less fetch or HTTP service does not receive an explicit spec,
Crosswalk can use its sealed Fabricator-compatible spreadsheet contract. That
contract retains the legacy column layout, staging roots `/home|/mnt`, media-use
term `151326`, and publication values `1`/`0`. New installations should prefer a
profile-derived, locally reviewed spec rather than relying on those historical
defaults.
