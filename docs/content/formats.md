# Format support

Crosswalk registers formats by stable command-line name. Parsing maps a source
document into Hub records; serialization maps Hub records to a target document.
Not every format needs both directions.

The registry is deterministic and safe for concurrent readers. Registration
fails explicitly when two adapters claim the same name, and automatic format
detection fails rather than choosing arbitrarily when more than one adapter
matches an input.

| Name | Description | Parse | Serialize |
|---|---|:---:|:---:|
| `archivesspace` | ArchivesSpace JSONModel snapshot/resource data | Yes | No |
| `arxiv` | arXiv Atom/OAI metadata | Yes | Yes |
| `bibtex` | BibTeX | Yes | Yes |
| `crossref` | Crossref deposit XML 5.3.1 | Yes | Yes |
| `crossref-rest` | Crossref REST API JSON response | Yes | No |
| `csl` | Citation Style Language JSON | Yes | Yes |
| `csv` | Generic or spec-driven CSV | Yes | Yes |
| `datacite` | DataCite XML 4.6 | Yes | Yes |
| `drupal` | Drupal/Islandora JSON | Yes | Yes |
| `dublincore` | Dublin Core | Yes | Yes |
| `islandora-workbench` | Islandora Workbench CSV | Yes | Yes |
| `mods` | MODS XML 3.8 | Yes | Yes |
| `omeka-s` | Omeka S JSON-LD acquisition snapshot | Yes | No |
| `proquest` | ProQuest ETD XML/delivery data | Yes | Yes |
| `schemaorg` | schema.org JSON-LD | Yes | Yes |
| `scopus` | Scopus Search API JSON | Yes | No |
| `wos` | Web of Science Starter API JSON | Yes | No |
| `zenodo` | Zenodo published-record API JSON | Yes | No |

Run `crosswalk convert --help` for conversion options and `crosswalk fetch
--help` for the API-backed acquisition commands.

## Crossref REST versus deposit XML

The two Crossref names have different wire contracts and responsibilities:

- `crossref-rest` parses the JSON response envelope returned by the Crossref
  REST API. It is an input adapter and has no serializer.
- `crossref` parses and writes Crossref deposit XML version 5.3.1. It is the
  deposit-format spoke.

Searching Crossref therefore flows through `crossref-rest`, the Hub,
reconciliation, and then the selected target serializer. Selecting `crossref`
as that target produces deposit XML; it does not preserve the REST response
envelope. See [Architecture](architecture.md#crossref-has-two-distinct-adapters).

## Dynamic systems

Drupal and Omeka S schemas differ by installation. Use a model-bound profile to
map their actual configured fields; see [Profiles and models](profiles.md).
Islandora Workbench additionally requires a sealed directional spec, described
in [Islandora Workbench](workbench.md).

ArchivesSpace currently uses a versioned JSONModel API adapter without an
installation profile. It preserves hierarchy in a Dataset and retains unknown
top-level plugin/newer fields in canonical JSON for later use.

## ProQuest embargo codes

For ProQuest ETD data, an explicit delayed-release date takes precedence. When
Crosswalk must derive the date from `embargo_code` and the acceptance date, the
supported codes are:

| Code | Delay |
|---|---|
| `0` | No embargo |
| `1` | 6 months |
| `2` | 12 months |
| `3` | 24 months |

The derived date is represented in the Hub as an `available` date and maps to
the Workbench embargo-until field. An invalid explicit embargo value falls back
to the code when possible.
