package hub

import (
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// RelationInverse returns the inverse relation type if one exists.
func RelationInverse(rt hubv1.RelationType) hubv1.RelationType {
	inverses := map[hubv1.RelationType]hubv1.RelationType{
		hubv1.RelationType_RELATION_TYPE_MEMBER_OF:        hubv1.RelationType_RELATION_TYPE_HAS_MEMBER,
		hubv1.RelationType_RELATION_TYPE_HAS_MEMBER:       hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
		hubv1.RelationType_RELATION_TYPE_PART_OF:          hubv1.RelationType_RELATION_TYPE_HAS_PART,
		hubv1.RelationType_RELATION_TYPE_HAS_PART:         hubv1.RelationType_RELATION_TYPE_PART_OF,
		hubv1.RelationType_RELATION_TYPE_VERSION_OF:       hubv1.RelationType_RELATION_TYPE_HAS_VERSION,
		hubv1.RelationType_RELATION_TYPE_HAS_VERSION:      hubv1.RelationType_RELATION_TYPE_VERSION_OF,
		hubv1.RelationType_RELATION_TYPE_REPLACES:         hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY,
		hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY:   hubv1.RelationType_RELATION_TYPE_REPLACES,
		hubv1.RelationType_RELATION_TYPE_FORMAT_OF:        hubv1.RelationType_RELATION_TYPE_HAS_FORMAT,
		hubv1.RelationType_RELATION_TYPE_HAS_FORMAT:       hubv1.RelationType_RELATION_TYPE_FORMAT_OF,
		hubv1.RelationType_RELATION_TYPE_REFERENCES:       hubv1.RelationType_RELATION_TYPE_IS_CITED_BY,
		hubv1.RelationType_RELATION_TYPE_IS_CITED_BY:      hubv1.RelationType_RELATION_TYPE_REFERENCES,
		hubv1.RelationType_RELATION_TYPE_CITES:            hubv1.RelationType_RELATION_TYPE_IS_CITED_BY,
		hubv1.RelationType_RELATION_TYPE_DERIVED_FROM:     hubv1.RelationType_RELATION_TYPE_SOURCE_OF,
		hubv1.RelationType_RELATION_TYPE_SOURCE_OF:        hubv1.RelationType_RELATION_TYPE_DERIVED_FROM,
		hubv1.RelationType_RELATION_TYPE_BASED_ON:         hubv1.RelationType_RELATION_TYPE_IS_BASIS_FOR,
		hubv1.RelationType_RELATION_TYPE_IS_BASIS_FOR:     hubv1.RelationType_RELATION_TYPE_BASED_ON,
		hubv1.RelationType_RELATION_TYPE_SUPPLEMENTS:      hubv1.RelationType_RELATION_TYPE_IS_SUPPLEMENT_TO,
		hubv1.RelationType_RELATION_TYPE_IS_SUPPLEMENT_TO: hubv1.RelationType_RELATION_TYPE_SUPPLEMENTS,
		hubv1.RelationType_RELATION_TYPE_DOCUMENTS:        hubv1.RelationType_RELATION_TYPE_IS_DOCUMENTED_BY,
		hubv1.RelationType_RELATION_TYPE_IS_DOCUMENTED_BY: hubv1.RelationType_RELATION_TYPE_DOCUMENTS,
		hubv1.RelationType_RELATION_TYPE_DESCRIBES:        hubv1.RelationType_RELATION_TYPE_IS_DESCRIBED_BY,
		hubv1.RelationType_RELATION_TYPE_IS_DESCRIBED_BY:  hubv1.RelationType_RELATION_TYPE_DESCRIBES,
		hubv1.RelationType_RELATION_TYPE_SERIES_OF:        hubv1.RelationType_RELATION_TYPE_IN_SERIES,
		hubv1.RelationType_RELATION_TYPE_IN_SERIES:        hubv1.RelationType_RELATION_TYPE_SERIES_OF,
	}

	if inv, ok := inverses[rt]; ok {
		return inv
	}
	return rt
}

// NormalizeRelationType normalizes a relation type string.
func NormalizeRelationType(value string) hubv1.RelationType {
	typeMap := map[string]hubv1.RelationType{
		"member_of":    hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
		"memberof":     hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
		"ismemberof":   hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
		"is_member_of": hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
		"has_member":   hubv1.RelationType_RELATION_TYPE_HAS_MEMBER,
		"hasmember":    hubv1.RelationType_RELATION_TYPE_HAS_MEMBER,
		"part_of":      hubv1.RelationType_RELATION_TYPE_PART_OF,
		"partof":       hubv1.RelationType_RELATION_TYPE_PART_OF,
		"ispartof":     hubv1.RelationType_RELATION_TYPE_PART_OF,
		"is_part_of":   hubv1.RelationType_RELATION_TYPE_PART_OF,
		"has_part":     hubv1.RelationType_RELATION_TYPE_HAS_PART,
		"haspart":      hubv1.RelationType_RELATION_TYPE_HAS_PART,
		"references":   hubv1.RelationType_RELATION_TYPE_REFERENCES,
		"cites":        hubv1.RelationType_RELATION_TYPE_CITES,
		"cited_by":     hubv1.RelationType_RELATION_TYPE_IS_CITED_BY,
		"iscitedby":    hubv1.RelationType_RELATION_TYPE_IS_CITED_BY,
		"version_of":   hubv1.RelationType_RELATION_TYPE_VERSION_OF,
		"versionof":    hubv1.RelationType_RELATION_TYPE_VERSION_OF,
		"has_version":  hubv1.RelationType_RELATION_TYPE_HAS_VERSION,
		"hasversion":   hubv1.RelationType_RELATION_TYPE_HAS_VERSION,
		"replaces":     hubv1.RelationType_RELATION_TYPE_REPLACES,
		"replaced_by":  hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY,
		"isreplacedby": hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY,
		"related_to":   hubv1.RelationType_RELATION_TYPE_RELATED_TO,
		"relatedto":    hubv1.RelationType_RELATION_TYPE_RELATED_TO,
		"related":      hubv1.RelationType_RELATION_TYPE_RELATED_TO,
		"derived_from": hubv1.RelationType_RELATION_TYPE_DERIVED_FROM,
		"derivedfrom":  hubv1.RelationType_RELATION_TYPE_DERIVED_FROM,
		"source_of":    hubv1.RelationType_RELATION_TYPE_SOURCE_OF,
		"sourceof":     hubv1.RelationType_RELATION_TYPE_SOURCE_OF,
		"series":       hubv1.RelationType_RELATION_TYPE_IN_SERIES,
		"in_series":    hubv1.RelationType_RELATION_TYPE_IN_SERIES,
		"inseries":     hubv1.RelationType_RELATION_TYPE_IN_SERIES,
	}

	if rt, ok := typeMap[value]; ok {
		return rt
	}
	return hubv1.RelationType_RELATION_TYPE_OTHER
}

// NewRelation creates a new Relation.
func NewRelation(relType hubv1.RelationType, targetTitle string) *hubv1.Relation {
	return &hubv1.Relation{
		Type:        relType,
		TargetTitle: targetTitle,
	}
}
