# Boundaries, migration, and integration status

Crosswalk deliberately replaces metadata semantics, not every operational
command from Fabricator, Papercut, or go-islandora. This page records the current
boundary so an absent mutation or connector is not mistaken for an implicit
feature.

## Current ownership boundaries

| Capability | Current owner or status |
|---|---|
| Google Sheet acquisition, permission changes, Apps Script deployment, and node-ID writeback | Not implemented by Crosswalk; download CSV through a trusted fixed-origin client, then use the CSV directly with the Workbench CLI or submit the same acquired grid/CSV to the matching authenticated HTTP endpoints |
| GitHub Actions scheduling and Slack notifications | Deployment orchestration outside Crosswalk |
| Drupal taxonomy-term lookup or creation | A Drupal-configured live Check My Work service queries mapping-declared term IDs, names, and URIs on the fixed selected site; offline transformation does not query, and Crosswalk never creates terms. A sealed policy may allow a positively missing plain name for creation by a later Workbench task |
| Duplicate metadata merge or repository update | Crosswalk emits reports and a manual review queue; it never merges automatically |
| General Workbench create/update/add-media execution | Not provided by Crosswalk; sitectl-isle currently exposes only its documented guarded jobs |
| Live node/entity, file, and Getty TGN validation | `crosswalk serve` installs fixed-origin read-only resolvers when an exact Drupal profile and JSON:API root are supplied; the standalone Workbench CLI remains deterministic-only, and local file checks require the service host to see the Workbench mount |
| Dynamic Drupal allowed values and non-`default:<entity_type>` entity-reference handlers | The sealed provider/handler and query contracts are validated, but checks fail explicitly until the selected site exposes a reviewed fixed adapter; this includes `views`, `views:*`, and every other custom handler. Crosswalk never executes a PHP callback name or uses a sheet value as an endpoint |
| Live Omeka S schema acquisition | Not implemented; supply a bounded schema snapshot |
| Live authenticated ArchivesSpace pagination | Not implemented; use snapshots or supplied resources |
| go-islandora Mirador cache, generic live export, and OpenAPI scaffolding | Not recreated |
| Papercut standalone licensing command | Not recreated; SHERPA enrichment is part of DOI acquisition |

The built-in Fabricator spreadsheet specification is a transition aid, not the
compatibility oracle for new development. New installations should compile a
profile-derived specification and declare their own mappings, validation rules,
paths, taxonomy terms, and publication policy.

## Workbench check scope

Crosswalk's Check My Work path covers the metadata and table rules represented
by the sealed transformation and frozen Drupal model. This includes canonical
header mapping, row shape, requiredness and operation applicability,
separator-aware cardinality, configured scalar text/date limits, static allowed
values, exact AttrItemBase `attr0` identifier patterns, structured Drupal field
grammars, sealed primary/supplemental and direct-field media-extension policy,
EDTF and workflow rules, created timestamp shape and non-future time, language
code enum membership, URL-alias leading slash and within-sheet uniqueness,
preceding parent references, and the shipped live node/entity/taxonomy/user,
URL-alias availability, file, and Getty context described in the HTTP service
documentation.

Islandora Workbench `--check` also inspects its own runtime configuration and
features that are not metadata-model rules. Crosswalk does not currently
replace checks for Workbench YAML syntax and required task options, Drupal
credentials or Integration module version, input/rollback path writability,
hook scripts, row filters and templates, remote-file accessibility, checksum
verification, OCR or media-track file content and encoding, or
media-use/derivative conflicts. URL-alias availability is checked only by the
live service against its fixed selected Drupal origin; the offline command
still performs shape and within-sheet uniqueness checks. Dynamic allowed-value
callbacks and every handler other than exact `default:<entity_type>` also
remain unavailable until a selected site supplies reviewed fixed adapters. A clean Crosswalk result therefore
means the declared metadata contract passed—not that an arbitrary Workbench
configuration or deployment is ready to mutate Drupal.

## CLI migration

The consolidation intentionally changed several authoring surfaces:

- the command group is `crosswalk profile`, not the former `profiles` form;
- the interactive `spoke` authoring commands were removed;
- installation-variable systems use reviewed models, profiles, and
  transformation specifications instead of generated static spokes; and
- profile flags are directional: use `--source-profile` when parsing a dynamic
  source and `--target-profile` when serializing to one. Workbench commands may
  expose a more specific target flag such as `--drupal-profile` as shown by
  their help.

Do not rewrite old automation by substituting command names mechanically.
Create and review the exact model/profile/spec chain first, then update scripts
to use the directional contract and its sealed fingerprints.

## Integration status

The automated suites exercise parsers, serializers, profiles, specifications,
protected HTTP behavior, archive handling, artifact planning, HTTP handlers, and
sitectl job logic. The principal remaining integration gap is a complete run
against the exact production versions of Drupal/Islandora, vendor APIs,
Islandora Workbench, storage mounts, and ingress configuration.

Before a production rollout:

1. Review a real Drupal or Omeka model and every generated mapping.
2. Confirm identifier namespaces, identity levels, and strong-evidence policy.
3. Confirm path roots, media-use term IDs, publication values, and contract
   deployment for the selected site.
4. Verify Workbench rollback row order before positional supplemental
   reconciliation; do not sort or filter either file between the create and
   reconciliation steps.
5. Test endpoint resolution in local and remote contexts, including degraded
   catalog fallback.
6. Confirm the active Workbench release's log messages against retry and
   rollback success validation.
7. Retain the source, sealed spec, contract revision, manifest, inputs, rollback
   artifact, and logs with the batch review record.

No artifact fingerprint or passing unit test authorizes a repository mutation.
Use a selected sitectl context, an independently deployed contract, trusted
artifact transport, deployment-aware preflight, and an explicit operator
decision.
