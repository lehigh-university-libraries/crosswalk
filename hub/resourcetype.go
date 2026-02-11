package hub

import (
	"strings"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

// ResourceTypeString returns the resource type as a string.
func ResourceTypeString(rt *hubv1.ResourceType) string {
	if rt.Original != "" {
		return rt.Original
	}
	return rt.Type.String()
}

// NormalizeResourceType maps common type strings to canonical types.
func NormalizeResourceType(value string) hubv1.ResourceTypeValue {
	lower := strings.ToLower(strings.TrimSpace(value))

	typeMap := map[string]hubv1.ResourceTypeValue{
		"article":             hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		"journal article":     hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		"journal-article":     hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		"book":                hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		"monograph":           hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK,
		"book chapter":        hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER,
		"book-chapter":        hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER,
		"chapter":             hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER,
		"conference paper":    hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		"conference-paper":    hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		"proceedings-article": hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER,
		"dataset":             hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		"data":                hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET,
		"dissertation":        hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		"doctoral thesis":     hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		"phd thesis":          hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION,
		"thesis":              hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		"master's thesis":     hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		"masters thesis":      hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS,
		"image":               hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE,
		"photograph":          hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE,
		"still image":         hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE,
		"journal":             hubv1.ResourceTypeValue_RESOURCE_TYPE_JOURNAL,
		"report":              hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT,
		"technical report":    hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT,
		"technical-report":    hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT,
		"working paper":       hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER,
		"working-paper":       hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER,
		"preprint":            hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		"poster":              hubv1.ResourceTypeValue_RESOURCE_TYPE_POSTER,
		"presentation":        hubv1.ResourceTypeValue_RESOURCE_TYPE_PRESENTATION,
		"software":            hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE,
		"video":               hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO,
		"moving image":        hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO,
		"audio":               hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO,
		"sound":               hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO,
		"map":                 hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP,
		"cartographic":        hubv1.ResourceTypeValue_RESOURCE_TYPE_MAP,
		"newspaper":           hubv1.ResourceTypeValue_RESOURCE_TYPE_NEWSPAPER,
		"newspaper article":   hubv1.ResourceTypeValue_RESOURCE_TYPE_NEWSPAPER_ARTICLE,
		"periodical":          hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL,
		"serial":              hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL,
		"collection":          hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION,
		"archival material":   hubv1.ResourceTypeValue_RESOURCE_TYPE_ARCHIVAL_MATERIAL,
		"manuscript":          hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT,
		"patent":              hubv1.ResourceTypeValue_RESOURCE_TYPE_PATENT,
		"standard":            hubv1.ResourceTypeValue_RESOURCE_TYPE_STANDARD,
		"web page":            hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE,
		"webpage":             hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE,
		"website":             hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE,
		"other":               hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER,
	}

	if rt, ok := typeMap[lower]; ok {
		return rt
	}

	// Partial matching
	if strings.Contains(lower, "thesis") {
		if strings.Contains(lower, "doctor") || strings.Contains(lower, "phd") {
			return hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION
		}
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS
	}
	if strings.Contains(lower, "article") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE
	}
	if strings.Contains(lower, "book") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	}
	if strings.Contains(lower, "conference") || strings.Contains(lower, "proceeding") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER
	}
	if strings.Contains(lower, "report") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT
	}
	if strings.Contains(lower, "image") || strings.Contains(lower, "photo") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	}
	if strings.Contains(lower, "video") || strings.Contains(lower, "film") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	}
	if strings.Contains(lower, "audio") || strings.Contains(lower, "sound") || strings.Contains(lower, "music") {
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	}

	return hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED
}

// NewResourceType creates a ResourceType with normalization.
func NewResourceType(original string, vocabulary string) *hubv1.ResourceType {
	return &hubv1.ResourceType{
		Type:       NormalizeResourceType(original),
		Original:   original,
		Vocabulary: vocabulary,
	}
}
