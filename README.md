# crosswalk

A CLI tool for converting scholarly metadata between formats using a hub-and-spoke architecture.

## Install

### Homebrew

You can install papercut using homebrew

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

# With a custom profile
crosswalk convert drupal csv -i export.json --profile my-site

# Create a profile from Drupal config
crosswalk profile create drupal my-site --from-config ./config/sync
```

## How It Works

```
Source Format    Profile           Hub Record         Rules            Target Format
─────────────    ───────           ──────────         ─────            ─────────────
Drupal JSON  →   field mappings →  Record      →      type mapping  →  schema.org
CSV          →   column mappings → (normalized) →     field rules   →  CrossRef XML
```

- **Profiles** define how source fields map to the hub schema (`~/.crosswalk/profiles/`)
- **Rules** define conditional transformations for output formats (`~/.crosswalk/rules/`)
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
| Web of Science      | planned | planned |
| Scopus              | planned | planned |

Have an idea for a new format? Issues and Pull Requests welcome!

## Future Work

- File support
  - Right now only metadata maps in the hub and spoke model. We can apply this same pattern to files
