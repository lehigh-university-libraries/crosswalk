# crosswalk

Crosswalk converts scholarly metadata between formats through a validated
hub-and-spoke data model. It supports local conversion, bounded metadata
acquisition, institution-specific Drupal and Omeka S profiles, duplicate
reconciliation, Islandora Workbench artifacts, and an authenticated HTTP
service.

## Quick start

Install with Go:

```bash
go install github.com/lehigh-university-libraries/crosswalk@latest
```

Or install the released binary with Homebrew:

```bash
brew tap lehigh-university-libraries/homebrew \
  https://github.com/lehigh-university-libraries/homebrew
brew install lehigh-university-libraries/homebrew/crosswalk
```

Convert Drupal JSON to CSV:

```bash
crosswalk convert drupal csv --input export.json --output records.csv
```

Use a published site profile when the source schema is installation-specific:

```bash
crosswalk convert drupal csv \
  --source-profile repository-items \
  --input export.json \
  --output records.csv
```

Run `crosswalk --help` for the complete command surface.

## Documentation

The project documentation lives at
<https://lehigh-university-libraries.github.io/crosswalk/>. Start with:

- [Architecture](https://lehigh-university-libraries.github.io/crosswalk/architecture/)
- [Profiles and models](https://lehigh-university-libraries.github.io/crosswalk/profiles/)
- [Existing-item reconciliation](https://lehigh-university-libraries.github.io/crosswalk/reconciliation/)
- [Islandora Workbench](https://lehigh-university-libraries.github.io/crosswalk/workbench/)
- [HTTP service](https://lehigh-university-libraries.github.io/crosswalk/http-service/)
- [Format support](https://lehigh-university-libraries.github.io/crosswalk/formats/)

For local development, see [CONTRIBUTING.md](CONTRIBUTING.md).
