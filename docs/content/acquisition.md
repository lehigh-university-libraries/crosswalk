# Acquisition and network safety

`crosswalk fetch` acquires bounded metadata or authorized media, parses it into
the Hub, reconciles existing-item evidence, and only then serializes accepted
records. Acquisition is separate from repository mutation: a fetch may write
local output or a Workbench artifact plan, but it never imports that plan into a
repository.

## Supported acquisition commands

| Command | Source | Optional media or enrichment |
|---|---|---|
| `fetch doi` | DOI content negotiation | Publisher PDF discovery/download, bounded public-directory email discovery, SHERPA policy enrichment |
| `fetch arxiv` | arXiv API or OAI-PMH | arXiv PDF download |
| `fetch crossref` | Crossref REST API | Selected target serialization after `crossref-rest` parsing |
| `fetch wos` | Web of Science Starter API | None |
| `fetch scopus` | Scopus Search API | None |
| `fetch zenodo` | Zenodo published-record API | Remote file metadata |
| `fetch proquest` | ProQuest XML, ZIP delivery, or recursive delivery directory | Primary PDF and supplemental-file staging |

Source-specific page, record, response, and file limits are shown by each
command's `--help`. All acquisition commands default to existing-item mode
`hold`; see [Existing-item reconciliation](reconciliation.md#safe-default-for-acquisition).

## Protected HTTP client

Remote metadata and media are untrusted even when a caller supplied the URL.
Protected requests use a common client with these invariants:

- only approved HTTP(S) destinations and source-specific origins are used;
- URL user information is rejected and credentials are redacted from errors;
- DNS results are checked and pinned before connection;
- private, loopback, link-local, multicast, unspecified, and other special
  destinations are rejected;
- redirects are bounded, revalidated, and do not forward authorization across
  origins;
- ambient proxy environment variables are not inherited by protected requests;
- status codes, response bodies, timeouts, and total work are bounded; and
- authorization is added only through the source client that owns the request.

These controls protect the process running Crosswalk. They do not turn an
arbitrary user URL into an approved acquisition source. New fetch commands must
define their origin and credential policy explicitly rather than accepting a
generic HTTP client that can bypass destination checks.

## Media and enrichment ordering

Crosswalk canonicalizes and validates records, then performs duplicate
reconciliation before SHERPA requests, PDF downloads, or ProQuest media
extraction. Held or skipped records therefore leave no acquisition files.

Downloads use bounded responses and explicit destination roots. A missing file
is reported according to the selected command policy; it is not silently
replaced by an unrelated URL or stale local file. Published output names the
file that was actually staged.

Email discovery is intentionally narrow: it is bounded to approved public
faculty/directory pages and is enrichment, not a general web crawler. SHERPA is
integrated into DOI acquisition; Crosswalk does not reproduce Papercut's
standalone licensing CLI.

## ProQuest delivery bundles

`fetch proquest` accepts an XML document, a delivery ZIP, or a directory tree of
deliveries. ZIP handling validates entries without extracting an untrusted
archive directly into the destination. It rejects:

- absolute paths and traversal;
- links and unsupported entry types;
- duplicate or ambiguous members;
- oversized members, total expansion, and excessive entry counts; and
- metadata/media changes between review and extraction.

Crosswalk first builds an immutable delivery snapshot and digest, parses and
reconciles its metadata, and verifies the same digest before media publication.
Accepted files are staged privately and published atomically. A failed, held,
or skipped item cannot leave a partially accepted media tree.

For embargoes, an explicit valid delayed-release date takes precedence over a
derived code. See [ProQuest embargo codes](formats.md#proquest-embargo-codes).

## Credentials and provenance

Vendor tokens and repository credentials belong in the environment variables
named by command help. Do not place credentials in a source URL, report,
specification, or command argument. Dataset and record provenance retain a safe
canonical source URI after credentials and fragments have been removed; see
[Hub records and datasets](hub-dataset.md#record-provenance).

## Not Google Sheets acquisition

Crosswalk does not fetch a Google Sheet, change its permissions, deploy Apps
Script, or write node IDs back to it. A trusted caller must acquire and
rectangularize the sheet, then submit JSON/CSV to the authenticated service.
See the [Google Sheets CSV workflow](http-service.md#google-sheets-csv-workflow).
