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

The check endpoint reports line breaks in taxonomy and multi-value cells while
allowing multiline prose fields. A resource type is required for ordinary
create rows when its spreadsheet column is present, but is optional for Page
and Sub-Collection object models.

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

Unknown spec keys, invalid mappings, stale fingerprints, or missing/mismatched
runtime profiles stop startup rather than silently changing behavior.

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

Transformation is side-effect-free. It does not contact or mutate Drupal, read
referenced media files, or use shared temporary files. The matches route is the
only Workbench route that may perform configured repository network requests,
and those are read-only. Each request builds its deterministic response in
memory.

The default listener is `127.0.0.1:8080`. A non-loopback plaintext bind is
rejected unless `--allow-public-http` is explicit; use that flag only behind a
trusted TLS-terminating proxy.

Request bodies, output, rows, cells, concurrent requests, headers, processing
time, socket reads/writes, keep-alive, and graceful shutdown are bounded.
Inspect the current defaults and flags with:

```bash
crosswalk serve --help
```
