# Contributing to Crosswalk

Build and test from the repository root:

```bash
make build
make lint
make test
```

Format Go changes with `make fmt`. Generated Protocol Buffer and JSON Schema
files are refreshed with `make install-tools generate`; do not edit `gen/`
directly.

The full contributor guide—including the Hub/spoke boundary, format-adapter
guidance, tests, and local documentation workflow—lives at
<https://lehigh-university-libraries.github.io/crosswalk/contributing/> and in
[docs/content/contributing.md](docs/content/contributing.md).
