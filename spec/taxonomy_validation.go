package spec

// configureDrupalTaxonomyNamePolicy records Workbench's allow_adding_terms
// behavior in the sealed field criteria. Numeric IDs and authority URIs are
// never covered by this policy: only a missing plain taxonomy-term name may be
// created by the subsequent Workbench task.
func configureDrupalTaxonomyNamePolicy(transformation *Transformation, allow bool) {
	if transformation == nil {
		return
	}
	for fieldIndex := range transformation.Source.Fields {
		for validationIndex := range transformation.Source.Fields[fieldIndex].Validations {
			validation := &transformation.Source.Fields[fieldIndex].Validations[validationIndex]
			if validation.Rule == ValidationContextEntityExists && validation.EntityType == "taxonomy_term" {
				validation.AllowNewNames = allow
			}
		}
	}
}
