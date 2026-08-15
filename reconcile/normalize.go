package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
)

var (
	htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
)

// IsStrongIdentifier reports whether an exact value is sufficient identity
// evidence under the built-in PolicyV1 identifier registry. Institution-scoped
// schemes require IsStrongIdentifierWithRegistry with an explicitly configured
// rule; unscoped local IDs, PIDs, and UUIDs are never exact by default.
func IsStrongIdentifier(id IdentifierKey) bool {
	return IsStrongIdentifierWithRegistry(id, hub.DefaultIdentifierRegistry())
}

// IsStrongIdentifierWithRegistry reports whether a canonical key is exact
// identity evidence under registry.
func IsStrongIdentifierWithRegistry(id IdentifierKey, registry *hub.IdentifierRegistry) bool {
	identifier := identifierFromKey(id)
	canonical, err := registry.CanonicalizeIdentifier(identifier)
	if err != nil || !registry.ExactIdentity(canonical) {
		return false
	}
	return identifierKey(canonical) == id
}

// Identifiers returns sorted, de-duplicated canonical work identifiers.
func Identifiers(record *hubv1.Record) []IdentifierKey {
	return identifiersWithRegistry(record, hub.DefaultIdentifierRegistry())
}

func identifiersWithRegistry(record *hubv1.Record, registry *hub.IdentifierRegistry) []IdentifierKey {
	if record == nil {
		return nil
	}

	ids := make([]IdentifierKey, 0, len(record.GetIdentifiers())+1)
	for _, id := range record.GetIdentifiers() {
		if id == nil {
			continue
		}
		candidate := id
		if id.GetScheme() == "" && (id.GetType() == hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED ||
			id.GetType() == hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL ||
			id.GetType() == hubv1.IdentifierType_IDENTIFIER_TYPE_URL) {
			detected := registry.DetectScheme(id.GetValue())
			if detected != "local" && detected != "url" {
				candidate = &hubv1.Identifier{
					Type: id.GetType(), Value: id.GetValue(), Display: id.GetDisplay(),
					IsPreferred: id.GetIsPreferred(), Scheme: detected,
					NamespaceUri: id.GetNamespaceUri(), IdentityLevel: id.GetIdentityLevel(),
				}
			}
		}
		if canonical, err := registry.CanonicalizeIdentifier(candidate); err == nil {
			ids = append(ids, identifierKey(canonical))
		} else if fallback := fallbackIdentifierKey(candidate, registry); fallback.Value != "" {
			ids = append(ids, fallback)
		}
	}

	if source := record.GetSourceInfo(); source != nil && source.GetSourceId() != "" {
		scheme := sourceIdentifierScheme(source.GetFormat(), source.GetSourceId(), registry)
		if scheme != "" {
			canonical, err := registry.NewIdentifierForScheme(source.GetSourceId(), scheme, hubv1.IdentifierIdentityLevel_IDENTIFIER_IDENTITY_LEVEL_UNSPECIFIED)
			if err == nil {
				ids = append(ids, identifierKey(canonical))
			}
		}
	}

	sort.Slice(ids, func(i, j int) bool { return identifierKeySortValue(ids[i]) < identifierKeySortValue(ids[j]) })
	return uniqueIdentifiers(ids)
}

// QueryFromRecord builds normalized lookup material for a record. Detector sets
// Strategy for each lookup pass.
func QueryFromRecord(key string, record *hubv1.Record) Query {
	return queryFromRecordWithRegistry(key, record, hub.DefaultIdentifierRegistry())
}

func queryFromRecordWithRegistry(key string, record *hubv1.Record, registry *hub.IdentifierRegistry) Query {
	snapshot := snapshot(record)
	return Query{
		Key:         key,
		Identifiers: identifiersWithRegistry(record, registry),
		Title:       preferredTitle(snapshot),
		Authors:     append([]string(nil), snapshot.Authors...),
		Year:        snapshot.Year,
	}
}

func sourceIdentifierScheme(format, sourceID string, registry *hub.IdentifierRegistry) string {
	format = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(format), "_", "-"))
	switch format {
	case "crossref", "cross-ref", "doi", "csl", "csl-json":
		if registry.DetectScheme(sourceID) == "doi" {
			return "doi"
		}
	case "arxiv":
		return "arxiv"
	case "wos", "web-of-science":
		return "wos"
	case "scopus":
		if scheme := registry.DetectScheme(sourceID); scheme == "scopus-eid" || scheme == "scopus-id" {
			return scheme
		}
	case "zenodo":
		if scheme := registry.DetectScheme(sourceID); scheme == "doi" || scheme == "zenodo-record" {
			return scheme
		}
	}
	return ""
}

func identifierKey(identifier *hubv1.Identifier) IdentifierKey {
	if identifier == nil {
		return IdentifierKey{}
	}
	return IdentifierKey{
		Scheme: identifier.GetScheme(), NamespaceURI: identifier.GetNamespaceUri(),
		Value: identifier.GetValue(), IdentityLevel: identifier.GetIdentityLevel(),
	}
}

func identifierFromKey(key IdentifierKey) *hubv1.Identifier {
	return &hubv1.Identifier{
		Scheme: key.Scheme, NamespaceUri: key.NamespaceURI,
		Value: key.Value, IdentityLevel: key.IdentityLevel,
	}
}

func fallbackIdentifierKey(identifier *hubv1.Identifier, registry *hub.IdentifierRegistry) IdentifierKey {
	if identifier == nil || strings.TrimSpace(identifier.GetValue()) == "" {
		return IdentifierKey{}
	}
	scheme := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(identifier.GetScheme()), "_", "-"))
	if scheme == "" {
		scheme = registry.DetectScheme(identifier.GetValue())
	}
	return IdentifierKey{
		Scheme: scheme, NamespaceURI: strings.TrimSpace(identifier.GetNamespaceUri()),
		Value: strings.TrimSpace(html.UnescapeString(identifier.GetValue())), IdentityLevel: identifier.GetIdentityLevel(),
	}
}

func identifierKeySortValue(key IdentifierKey) string {
	return strings.Join([]string{key.Scheme, key.NamespaceURI, key.IdentityLevel.String(), key.Value}, "\x00")
}

func canonicalURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimSpace(value)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if (parsed.Scheme == "https" && strings.HasSuffix(parsed.Host, ":443")) ||
		(parsed.Scheme == "http" && strings.HasSuffix(parsed.Host, ":80")) {
		parsed.Host = parsed.Host[:strings.LastIndex(parsed.Host, ":")]
	}
	if parsed.Path != "/" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return parsed.String()
}

func uniqueIdentifiers(values []IdentifierKey) []IdentifierKey {
	if len(values) == 0 {
		return nil
	}
	result := values[:0]
	var previous IdentifierKey
	for i, value := range values {
		if i == 0 || value != previous {
			result = append(result, value)
			previous = value
		}
	}
	return result
}

func snapshot(record *hubv1.Record) Snapshot {
	return snapshotWithRegistry(record, hub.DefaultIdentifierRegistry())
}

func snapshotWithRegistry(record *hubv1.Record, registry *hub.IdentifierRegistry) Snapshot {
	if record == nil {
		return Snapshot{}
	}
	publishers := hub.GetPublishers(record)
	languages := hub.GetLanguages(record)
	result := Snapshot{
		Title:        cleanDisplay(record.GetTitle()),
		FullTitle:    cleanDisplay(record.GetFullTitle()),
		Authors:      authorDisplayNames(record),
		Identifiers:  identifiersWithRegistry(record, registry),
		Publisher:    cleanDisplay(firstCompatibilityValue(publishers)),
		Language:     cleanDisplay(firstCompatibilityValue(languages)),
		ResourceType: resourceType(record.GetResourceType()),
	}
	result.Year = primaryYear(record)
	if abstract := strings.TrimSpace(record.GetAbstract()); abstract != "" {
		digest := sha256.Sum256([]byte(abstract))
		result.AbstractLength = utf8.RuneCountInString(abstract)
		result.AbstractSHA256 = hex.EncodeToString(digest[:])
	}
	return result
}

func firstCompatibilityValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func cleanDisplay(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(value)), " ")
}

func resourceType(value *hubv1.ResourceType) string {
	if value == nil {
		return ""
	}
	if original := cleanDisplay(value.GetOriginal()); original != "" {
		return original
	}
	name := strings.TrimPrefix(value.GetType().String(), "RESOURCE_TYPE_")
	if name == "UNSPECIFIED" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(name, "_", "-"))
}

func authorContributors(record *hubv1.Record) []*hubv1.Contributor {
	if record == nil {
		return nil
	}
	var authors []*hubv1.Contributor
	for _, contributor := range record.GetContributors() {
		if contributor == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(contributor.GetRole()))
		roleCode := strings.ToLower(strings.TrimSpace(contributor.GetRoleCode()))
		if role == "author" || role == "creator" || role == "aut" || role == "cre" ||
			strings.HasSuffix(roleCode, ":aut") || strings.HasSuffix(roleCode, ":cre") ||
			roleCode == "aut" || roleCode == "cre" {
			authors = append(authors, contributor)
		}
	}
	if len(authors) != 0 {
		return authors
	}
	for _, contributor := range record.GetContributors() {
		if contributor != nil && contributorName(contributor) != "" {
			authors = append(authors, contributor)
		}
	}
	return authors
}

func authorDisplayNames(record *hubv1.Record) []string {
	seen := make(map[string]bool)
	var names []string
	for _, contributor := range authorContributors(record) {
		name := cleanDisplay(contributorName(contributor))
		canonical := normalizeText(name)
		if name == "" || canonical == "" || numericOnly(canonical) || seen[canonical] {
			continue
		}
		seen[canonical] = true
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	if len(names) == 0 {
		return nil
	}
	return names
}

func contributorName(contributor *hubv1.Contributor) string {
	if contributor == nil {
		return ""
	}
	if name := strings.TrimSpace(contributor.GetName()); name != "" {
		return name
	}
	if parsed := contributor.GetParsedName(); parsed != nil {
		if parsed.GetFullName() != "" {
			return parsed.GetFullName()
		}
		if parsed.GetNormalized() != "" {
			return parsed.GetNormalized()
		}
		parts := []string{parsed.GetGiven(), parsed.GetMiddle(), parsed.GetFamily(), parsed.GetSuffix()}
		return strings.Join(nonempty(parts), " ")
	}
	return ""
}

type normalizedAuthor struct {
	Full   string
	Family string
	ORCIDs []string
}

func normalizedAuthors(record *hubv1.Record) []normalizedAuthor {
	var result []normalizedAuthor
	seen := make(map[string]bool)
	for _, contributor := range authorContributors(record) {
		name := contributorName(contributor)
		if name == "" {
			continue
		}
		parsed := contributor.GetParsedName()
		if parsed == nil || parsed.GetFamily() == "" {
			parsed = helpers.ParseName(name)
		}
		full := normalizeText(name)
		family := ""
		if parsed != nil {
			family = normalizeText(parsed.GetFamily())
			given := normalizeText(parsed.GetGiven())
			middle := normalizeText(parsed.GetMiddle())
			if family != "" && given != "" {
				full = strings.Join(nonempty([]string{family, given, middle}), "|")
			}
		}
		if full == "" || numericOnly(full) {
			continue
		}
		var orcids []string
		for _, id := range contributor.GetIdentifiers() {
			if id == nil || id.GetType() != hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
				continue
			}
			canonical, err := hub.DefaultIdentifierRegistry().CanonicalizeIdentifier(id)
			if err == nil && canonical.GetScheme() == "orcid" {
				orcids = append(orcids, canonical.GetValue())
			}
		}
		sort.Strings(orcids)
		orcids = uniqueStrings(orcids)
		key := full + "\x00" + strings.Join(orcids, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, normalizedAuthor{Full: full, Family: family, ORCIDs: orcids})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Full == result[j].Full {
			return strings.Join(result[i].ORCIDs, "\x00") < strings.Join(result[j].ORCIDs, "\x00")
		}
		return result[i].Full < result[j].Full
	})
	return result
}

func normalizeTitle(value string) string {
	value = html.UnescapeString(value)
	value = htmlTagPattern.ReplaceAllString(value, " ")
	return normalizeText(value)
}

func normalizeText(value string) string {
	var builder strings.Builder
	space := true
	for _, char := range strings.ToLower(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(char)
			space = false
		} else if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func numericOnly(value string) bool {
	foundDigit := false
	for _, char := range value {
		switch {
		case unicode.IsDigit(char):
			foundDigit = true
		case unicode.IsLetter(char):
			return false
		}
	}
	return foundDigit
}

func preferredTitle(value Snapshot) string {
	if value.FullTitle != "" {
		return value.FullTitle
	}
	return value.Title
}

func titleVariants(record *hubv1.Record) []string {
	if record == nil {
		return nil
	}
	values := []string{normalizeTitle(record.GetTitle()), normalizeTitle(record.GetFullTitle())}
	values = nonempty(values)
	sort.Strings(values)
	return uniqueStrings(values)
}

func primaryYear(record *hubv1.Record) int32 {
	if record == nil {
		return 0
	}
	if date := hub.PrimaryDate(record); date != nil && date.GetYear() != 0 {
		return date.GetYear()
	}
	for _, date := range record.GetDates() {
		if date != nil && date.GetYear() != 0 {
			return date.GetYear()
		}
	}
	return 0
}

func nonempty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := values[:0]
	previous := ""
	for i, value := range values {
		if i == 0 || value != previous {
			result = append(result, value)
			previous = value
		}
	}
	return result
}

func formatYear(year int32) string {
	if year == 0 {
		return ""
	}
	return strconv.FormatInt(int64(year), 10)
}
