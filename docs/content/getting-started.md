# Getting started

## Install

Install the latest source release with Go:

```bash
go install github.com/lehigh-university-libraries/crosswalk@latest
```

Homebrew users can install the released binary:

```bash
brew tap lehigh-university-libraries/homebrew \
  https://github.com/lehigh-university-libraries/homebrew
brew install lehigh-university-libraries/homebrew/crosswalk
```

Prebuilt binaries are also available from the
[latest GitHub release](https://github.com/lehigh-university-libraries/crosswalk/releases/latest).
Put the downloaded `crosswalk` executable in a directory on `PATH`.

## Convert a document

`convert` takes a source format and target format. Input and output default to
standard input and standard output:

```bash
crosswalk convert drupal csv \
  --input export.json \
  --output records.csv
```

The same conversion can be streamed:

```bash
cat export.json | crosswalk convert drupal csv >records.csv
```

Use `--pretty` for target formats that support formatted output, `--separator`
to choose a tabular multi-value separator, and `--base-url` only when a source
adapter must resolve relative identifiers. See the available options with:

```bash
crosswalk convert --help
```

## Use an installation-specific profile

Drupal and Omeka S fields vary by installation. A published profile binds
ordered mappings and identity policy to the exact model snapshot from which it
was authored:

```bash
crosswalk convert drupal csv \
  --source-profile repository-items \
  --input export.json \
  --output records.csv
```

Profiles and their content-addressed models live below the Crosswalk
configuration directory, `$HOME/.crosswalk` by default. Use `--config-dir` on
any command to select another directory. See [Profiles and models](profiles.md)
before creating or publishing one.

## Produce Islandora Workbench data

Workbench output is controlled by a sealed transformation specification. For a
site profile, compile the draft, add deployment policy, validate it, and use
the exact same profile at serialization time:

```bash
crosswalk spec compile drupal \
  --profile repository-items \
  --output islandora-object-spec.draft.yaml

# Review paths, local term IDs, and mappings in the draft.
crosswalk spec validate \
  --input islandora-object-spec.draft.yaml \
  --output islandora-object-spec.yaml

crosswalk convert csv islandora-workbench \
  --spec islandora-object-spec.yaml \
  --target-profile repository-items \
  --input metadata.csv \
  --output target.csv
```

See [Transformation specifications](specifications.md) for the complete mapping
and validation schema, and [Islandora Workbench](workbench.md) for artifact
planning, media policy, and the independently provisioned sitectl contract.

## Acquire scholarly metadata

`fetch` provides bounded acquisition for DOI content negotiation, arXiv,
Crossref REST, Web of Science, Scopus, Zenodo, and ProQuest deliveries. These
commands default to existing-item mode `hold`, which requires a Drupal
JSON:API endpoint and published Drupal profile. Use `assume-new` only for an
input known to contain new records:

```bash
crosswalk fetch crossref --help
crosswalk fetch scopus --help
crosswalk fetch zenodo --help
```

See [Acquisition and network safety](acquisition.md) for source and media trust
boundaries, then [Existing-item reconciliation](reconciliation.md) before
running a batch.
