package cmd

import (
	"fmt"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/profile"
	"github.com/lehigh-university-libraries/crosswalk/reconcile"
	"github.com/lehigh-university-libraries/crosswalk/spec"
)

type drupalReconciliationProfile struct {
	compiled *profile.Compiled
	// transformation is compiled from the exact immutable model paired with
	// compiled. It prevents output from claiming this profile while silently
	// falling back to an unrelated built-in Workbench schema.
	transformation *spec.Transformation
	policy         reconcile.Policy
	provenance     reconcile.ReportProvenance
}

func loadDrupalReconciliationProfile(name string, requireLookup bool) (*drupalReconciliationProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("drupal profile name is required")
	}
	stored, err := profile.LoadStored(name)
	if err != nil {
		return nil, fmt.Errorf("loading Drupal profile %q: %w", name, err)
	}
	if stored == nil || stored.Compiled == nil || stored.Model == nil {
		return nil, fmt.Errorf("loading Drupal profile %q: stored profile is incomplete", name)
	}
	compiled := stored.Compiled
	if compiled.System() != "drupal" || stored.Model.System != "drupal" {
		return nil, fmt.Errorf("profile %q uses system %q, not drupal", name, compiled.System())
	}
	if requireLookup && !compiled.LookupPlan().Enabled {
		return nil, fmt.Errorf("drupal profile %q has no enabled existing-item lookup policy", name)
	}
	policy, err := reconcile.NewPolicy(compiled.IdentifierRegistryConfig())
	if err != nil {
		return nil, fmt.Errorf("building reconciliation policy from Drupal profile %q: %w", name, err)
	}
	bundle, err := drupalProfileBundle(stored.Definition)
	if err != nil {
		return nil, fmt.Errorf("building Workbench transformation from Drupal profile %q: %w", name, err)
	}
	transformation, err := spec.CompileDrupalProfile(stored.Model, compiled, spec.DrupalCompileOptions{Bundle: bundle})
	if err != nil {
		return nil, fmt.Errorf("building Workbench transformation from Drupal profile %q: %w", name, err)
	}
	return &drupalReconciliationProfile{
		compiled:       compiled,
		transformation: transformation,
		policy:         policy,
		provenance: reconcile.ReportProvenance{
			System:             compiled.System(),
			ProfileName:        compiled.Name(),
			ProfileFingerprint: compiled.Fingerprint(),
			ModelFingerprint:   compiled.ModelFingerprint(),
		},
	}, nil
}

func drupalProfileBundle(definition *profile.Definition) (string, error) {
	if definition == nil {
		return "", fmt.Errorf("profile definition is missing")
	}
	bundle := ""
	for _, mapping := range definition.Mappings {
		if mapping.Field.EntityType != "node" || strings.TrimSpace(mapping.Field.Bundle) == "" {
			return "", fmt.Errorf("mapping field %q does not select a Drupal node bundle", mapping.Field.Path)
		}
		if bundle == "" {
			bundle = mapping.Field.Bundle
		} else if mapping.Field.Bundle != bundle {
			return "", fmt.Errorf("profile spans Drupal node bundles %q and %q", bundle, mapping.Field.Bundle)
		}
	}
	if bundle == "" {
		return "", fmt.Errorf("profile has no Drupal node mappings")
	}
	return bundle, nil
}
