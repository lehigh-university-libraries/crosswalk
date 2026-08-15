# Contributing

## Build and test

From the repository root:

```bash
make build
make test
```

Before opening a pull request, format the code and run the relevant checks:

```bash
make fmt
make lint
make test
```

Generated Protocol Buffer and JSON Schema artifacts are refreshed with:

```bash
make install-tools
make generate
```

Do not hand-edit files below `gen/`.

## Add or change a format

A format adapter is a Hub spoke. Keep wire-format behavior in its own package
and map into or out of `hubv1.Record`; do not add a direct source-to-target
converter. Implement `format.Parser`, `format.Serializer`, or both, register the
format, and add fixtures and focused tests.

Static standards may use a versioned spoke Protocol Buffer model. Dynamic
systems should use a content-addressed model/profile when field semantics vary
by installation. Keep repository acquisition and mutation out of profile
compilation.

Crossref changes require special care:

- REST API JSON parsing belongs in `format/crossrefrest`.
- Crossref deposit XML parsing/serialization belongs in `format/crossref` and
  its versioned deposit spoke.

See [Architecture](architecture.md) for the complete boundary.

## Tests

Add fixtures under `fixtures/<format>/` or package-local `testdata/` and verify
the behavior relevant to the direction implemented:

- parsing extracts canonical Hub fields and provenance;
- serialization writes the expected target contract;
- invalid, oversized, ambiguous, or unsupported inputs fail closed;
- round trips preserve representable data;
- profile-bound behavior checks exact profile and model fingerprints;
- identifier tests include scheme, namespace, identity level, canonicalization,
  and whole-value pattern semantics.

Run focused tests while developing, followed by the complete suite:

```bash
go test ./format/example
go test ./...
go test -race ./...
```

## Documentation

Crosswalk's documentation is a Zensical site built from `docs/content/`. The
build environment and Zensical version are pinned in `docs/Dockerfile`.

Build the static site into `docs/site/`:

```bash
make docs-build
```

Serve with live reload at <http://localhost:8888>:

```bash
make docs-serve
```

Build the production artifact and serve that directory with Python's HTTP
server:

```bash
make docs-preview
```

Override `DOCS_PORT`, `DOCS_IMAGE`, or `SITE_URL` as needed. Remove generated
output with:

```bash
make docs-clean
```

Add every new page to the explicit `nav` in `docs/mkdocs.yml`, keep links
relative inside the documentation site, and run `make docs-build` before
opening a pull request.
