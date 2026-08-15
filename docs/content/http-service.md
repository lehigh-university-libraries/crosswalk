# HTTP service

`crosswalk serve` exposes the spreadsheet validation, Workbench transformation,
and optional existing-item review functions over authenticated HTTP. It replaces
Fabricator's metadata endpoints while keeping repository mutation out of the
service.

## Authentication is mandatory

The server refuses to start unless at least one complete authentication
mechanism is configured.

### Shared secret

The simplest configuration reads a non-empty secret from `SHARED_SECRET`:

```bash
export SHARED_SECRET='replace-with-a-deployment-secret'
crosswalk serve
```

Clients send it in `X-Secret`:

```bash
curl --fail --silent \
  --header "X-Secret: $SHARED_SECRET" \
  --header 'Content-Type: application/json' \
  --data-binary @rows.json \
  http://127.0.0.1:8080/workbench/check
```

Use `--shared-secret-env` to select another environment variable.

### Google identity token

Google Apps Script can send the OpenID Connect token returned by
`ScriptApp.getIdentityToken()` as `Authorization: Bearer <token>`. Configure
the exact audience and at least one identity policy:

```bash
export CROSSWALK_GOOGLE_AUDIENCE='123456.apps.googleusercontent.com'
export CROSSWALK_GOOGLE_HOSTED_DOMAIN='example.edu'
# Optional exceptions, or an alternative to the hosted-domain policy:
export CROSSWALK_GOOGLE_ALLOWED_EMAILS='person@example.edu,other@example.edu'
crosswalk serve
```

Equivalent flags are `--google-audience`, `--google-hosted-domain`, and
`--google-allowed-emails`; explicit flags take precedence over the environment.
When domain and email policies are both present, a verified identity may match
either. Google and shared-secret mechanisms may coexist during migration.

The Apps Script manifest must request the `openid` and
`https://www.googleapis.com/auth/userinfo.email` scopes. The configured
audience is the token's `aud` claim—normally the script project's OAuth client
ID—not the Crosswalk service URL.

Crosswalk verifies the signature, issuer, exact audience, expiration, optional
not-before time, `email_verified`, and configured identity policy locally.
Google's JWKS is fetched with a bounded client at startup and refreshed from
cache; tokens are not sent to the `tokeninfo` endpoint.

## Endpoints

| Method | Path | Request | Response |
|---|---|---|---|
| `GET` | `/healthcheck` | None | `text/plain` |
| `POST` | `/workbench/check` | JSON array of spreadsheet rows | JSON cell-to-error map |
| `POST` | `/workbench/transform` | CSV spreadsheet export | ZIP of Workbench CSV artifacts and manifest |
| `POST` | `/workbench/matches?mode=hold` | CSV spreadsheet export | ZIP containing the JSON report and review CSV |

All Workbench routes require one of the configured authentication mechanisms.
The healthcheck does not.

```bash
curl --fail --silent \
  --header "X-Secret: $SHARED_SECRET" \
  --header 'Content-Type: text/csv' \
  --data-binary @source.csv \
  --output workbench.zip \
  http://127.0.0.1:8080/workbench/transform
```

The check endpoint evaluates parser and Hub invariants plus the deterministic
field/table rules declared by the active transformation specification. Rules
reference canonical machine field names; human labels and aliases only resolve
headers and identify cells in diagnostics. Without `--drupal-jsonapi`, the
service is deterministic-only. Supplying `--drupal-jsonapi` and the exact
`--drupal-profile` installs Crosswalk's read-only live validation context for
the selected Drupal model, local staging filesystem, and fixed Getty TGN
authority. Merely declaring a context rule in a specification never supplies
an endpoint or grants access by itself. See
[Transformation specifications](specifications.md#validation-rules).

## Google Sheets CSV workflow

Crosswalk accepts an already acquired string table or CSV; it does not dereference
a Google sharing URL. A Google-based caller should keep acquisition,
deterministic metadata validation, and repository operation as separate trust
boundaries:

1. Accept only the expected `https://docs.google.com/spreadsheets/d/ID` sharing
   form and extract an ID made entirely of the allowed Google identifier
   characters. Do not fetch the supplied sharing URL.
2. Validate the selected sheet/range as data, percent-encode it as a path
   component, and call the fixed `https://sheets.googleapis.com` API origin with
   a read-only Sheets access token.
3. Apply connection and request timeouts, bounded retries, response-size limits,
   and an exact 2xx requirement. Decode one JSON value and require the expected
   `values` array; a non-empty error document is not sheet data.
4. Convert every scalar cell to a string, find the maximum row width, and pad
   shorter rows with empty strings. Preserve quoted commas, embedded newlines in
   prose, and empty trailing cells.
5. Send that exact rectangular grid as JSON to `/workbench/check`. Require both
   a successful HTTP response and an empty cell-error map.
6. Encode the same grid with a real CSV writer and send it to
   `/workbench/transform`. Do not use physical line counts, substring header
   searches, or a second normalization path.
7. Treat the returned ZIP and every CSV as untrusted input at the client
   boundary. Bound archive size and entries, reject traversal and duplicate
   names, and validate the manifest and independently provisioned contract
   before any context operation.
8. Run required sitectl context preflight and an explicitly authorized
   Workbench operation. Retain the normalized source, check result, manifest,
   contract revision, inputs, rollback artifact, and logs.

The Google Sheets API access token and the OIDC identity token used to
authenticate to Crosswalk are different credentials with different audiences.
Do not send a Sheets bearer token to Crosswalk or a Crosswalk identity token to
the Sheets API.

Crosswalk intentionally does not change Sheet permissions, append node IDs,
send Slack messages, or dispatch a GitHub Actions workflow. Those are optional
orchestration concerns outside metadata validation; see
[Boundaries, migration, and integration status](limitations.md).

## Site-specific contract

Start the service with a published Drupal profile and its exact reviewed spec:

```bash
crosswalk serve \
  --drupal-profile repository-items \
  --spec islandora-object-spec.yaml
```

With `--drupal-profile`, the default transformation is compiled from that
profile's stored model and mappings. An explicit `--spec` must be bound to the
same exact profile and model. Without either option, the service uses the
sealed Fabricator compatibility contract described in
[Islandora Workbench](workbench.md#compatibility-contract).

When the service compiles that profile-derived transformation,
`--workbench-allow-new-taxonomy-terms` defaults to true for Fabricator parity.
Set `--workbench-allow-new-taxonomy-terms=false` when every referenced taxonomy
term must already exist. The option permits only missing plain names for later
creation by Workbench; numeric IDs and authority URIs must still resolve. If
`--spec` is supplied, its sealed `allow_new_names` values are authoritative and
the server does not rewrite them.

Unknown spec keys, invalid mappings, stale fingerprints, or missing/mismatched
runtime profiles stop startup rather than silently changing behavior.

## Live Check My Work context

Supplying the resolved JSON:API root changes `/workbench/check` from
deterministic-only validation to the shipped live context configured by
`crosswalk serve`:

```bash
export DRUPAL_JSONAPI_TOKEN='read-only-token'

crosswalk serve \
  --drupal-jsonapi https://repository.example.edu/jsonapi \
  --drupal-profile repository-items \
  --spec islandora-object-spec.yaml \
  --workbench-staging-root /mnt/islandora_staging \
  --workbench-allowed-absolute-roots '/home|/mnt'
```

The profile and its content-addressed model must match the sealed spec exactly.
The live resolver then checks mapping-declared node and entity references,
including taxonomy term IDs, plain names, and authority URIs, URL-alias
availability on the fixed selected Drupal origin, local staged-file
readability, and numeric Getty TGN identifiers. Repeated values are memoized
only for the current request, while each affected cell still receives its own
finding. A missing plain taxonomy name passes only when its sealed rule has
`allow_new_names: true`; resolver failures, IDs, and URIs never use that
exception.

Each live check has two independent default work budgets shared by every
context resolver: at most 4,096 distinct context lookups and at most 4,096
outbound validation requests. A memoized repeat does not consume another
lookup, but every actual Drupal or Getty request consumes the outbound budget.
Taxonomy bundle and URI-route fan-out and Getty requests all count toward that
same outbound limit. Exceeding either budget fails the check before the next
lookup or request is executed.

The filesystem options may also come from
`CROSSWALK_WORKBENCH_STAGING_ROOT` and
`CROSSWALK_WORKBENCH_ALLOWED_ABSOLUTE_ROOTS`. Explicit flags take precedence,
then those environment variables, then the sealed spec defaults. If the spec
has no staging root, Crosswalk uses `/mnt/islandora_staging`; additional
absolute roots have no extra fallback. The additional-root value is
pipe-delimited. Every root must be a clean absolute path below, but not equal
to, a filesystem volume root.

These path defaults are executable transformation policy and therefore affect
the spec fingerprint recorded in an artifact manifest. For an artifact bundle
that will be checked against a sitectl contract, put the reviewed values in the
sealed spec and provision the contract from that same spec. Do not provision a
contract from pre-override values and then change them only at server startup.

File checks run as the user and on the host running the Crosswalk process. That
host must therefore have the same staging mount Workbench will read. Resolving
a remote sitectl context supplies a Drupal URL; it does not make that remote
filesystem visible to a locally running Crosswalk process. When Crosswalk is
not colocated with the Workbench mount, run sitectl-isle
`workbench-preflight` in the selected context instead of treating a local
result as proof of remote readability. Crosswalk rejects paths outside the
configured roots, symlink components, directories, missing files, and files
the effective process user cannot open.

Repository queries are derived only from the selected model's machine entity,
bundle, field, and handler data. Candidate IDs, names, and URIs are encoded as
query values on the configured Drupal origin; a spreadsheet URI is never
fetched as a destination. Taxonomy URI lookup uses only the selected site's
fixed `term_from_uri` and `term_from_authority_link` routes. Getty validation
accepts a numeric identifier and requests only
`https://vocab.getty.edu/tgn/ID.json` through Crosswalk's protected, bounded,
no-proxy HTTP client.

URL-alias availability always queries the fixed JSON:API
`path_alias/path_alias` collection beneath the configured Drupal JSON:API root.
The candidate alias is URL-encoded only as the exact `alias` filter value; it
never becomes a request-path segment, request path, host, or destination.

A model-declared Drupal `allowed_values_function` is bound and verified, but
Crosswalk never executes its PHP callback name. Until the selected site exposes
a reviewed fixed validation adapter, a row requiring that callback fails the
check explicitly with an unavailable-capability error. The shipped
entity-reference resolver accepts only Drupal core's exact
`default:<entity_type>` handler, where `<entity_type>` equals the sealed query's
entity type. `views`, `views:*`, and every other custom handler fail closed
pending a site-specific fixed adapter. Neither case silently accepts unchecked
data.

## Read-only repository matching

Enable `/workbench/matches` against Drupal by supplying both the resolved
JSON:API route and site profile:

```bash
crosswalk serve \
  --drupal-jsonapi https://repository.example.edu/jsonapi \
  --drupal-profile repository-items \
  --spec islandora-object-spec.yaml
```

The endpoint performs bounded read-only queries using the profile's entity,
lookup fields, identifier authorities, and identity policy. Credentials may
come from `DRUPAL_JSONAPI_TOKEN`, or the Basic-auth environment variables shown
by `crosswalk serve --help`. Configuring a live endpoint without a valid Drupal
profile fails before listening.

Modes are `hold`, `skip`, `force-new`, and `assume-new`. See
[Existing-item reconciliation](reconciliation.md) for their semantics.

## Operational boundary and limits

Transformation is side-effect-free: `/workbench/transform` does not contact or
mutate Drupal, read referenced media files, or use shared temporary files. In a
live-context deployment, `/workbench/check` performs bounded read-only Drupal,
Getty, and local-filesystem checks; `/workbench/matches` performs bounded
read-only duplicate lookup. No route executes Workbench or mutates the
repository. Each response is built in memory.

The default listener is `127.0.0.1:8080`. A non-loopback plaintext bind is
rejected unless `--allow-public-http` is explicit; use that flag only behind a
trusted TLS-terminating proxy.

Request bodies, output, rows, cells, concurrent requests, headers, processing
time, socket reads/writes, keep-alive, and graceful shutdown are bounded.
Inspect the current defaults and flags with:

```bash
crosswalk serve --help
```
