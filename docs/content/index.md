# Crosswalk

Crosswalk converts scholarly metadata between formats through a validated
hub-and-spoke data model. Source adapters parse into `hubv1.Record`; optional
reconciliation evaluates existing-item evidence; target adapters serialize the
accepted records.

Crosswalk owns metadata semantics: the Hub schema, format adapters, immutable
models and profiles, transformation specifications, and reconciliation policy.
`sitectl` owns repository operations such as acquiring live Drupal
configuration and running the resulting Islandora Workbench jobs. Crosswalk
does not mutate a Drupal, Omeka S, or ArchivesSpace repository.

## Quick start

```bash
go install github.com/lehigh-university-libraries/crosswalk@latest

crosswalk convert drupal csv \
  --input export.json \
  --output records.csv
```

For an installation-specific Drupal schema, use a reviewed, published profile:

```bash
crosswalk convert drupal csv \
  --source-profile repository-items \
  --input export.json \
  --output records.csv
```

## Documentation

- [Getting started](getting-started.md) covers installation and basic conversion.
- [Architecture](architecture.md) explains the Hub, adapters, profiles, specs, and ownership boundaries.
- [Hub records and datasets](hub-dataset.md) documents canonical record, hierarchy, and provenance contracts.
- [Profiles and models](profiles.md) covers Drupal and Omeka S profile lifecycles.
- [Transformation specifications](specifications.md) is the mapping and validation-rule reference.
- [Acquisition and network safety](acquisition.md) covers fetch commands, protected HTTP, and delivery bundles.
- [Existing-item reconciliation](reconciliation.md) explains identifier-first duplicate detection and review.
- [Islandora Workbench](workbench.md) covers profile-bound specifications, artifacts, and the sitectl contract.
- [HTTP service](http-service.md) documents authentication and service endpoints.
- [Format support](formats.md) lists parse and serialization capabilities.
- [Boundaries, migration, and integration status](limitations.md) records intentional omissions and rollout checks.
- [Contributing](contributing.md) covers repository and documentation workflows.
