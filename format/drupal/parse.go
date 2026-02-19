package drupal

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
	"github.com/lehigh-university-libraries/crosswalk/hub"
	"github.com/lehigh-university-libraries/crosswalk/mapping"
)

// Parse reads Drupal JSON and returns hub records.
func (f *Format) Parse(r io.Reader, opts *format.ParseOptions) ([]*hubv1.Record, error) {
	if opts == nil {
		opts = format.NewParseOptions()
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	data = trimBOM(data)

	// Determine if input is a single entity or array
	var entities []DrupalEntity

	data = skipWhitespace(data)
	if len(data) == 0 {
		return nil, nil
	}

	switch data[0] {
	case '[':
		// Array of entities
		if err := json.Unmarshal(data, &entities); err != nil {
			return nil, fmt.Errorf("parsing JSON array: %w", err)
		}
	case '{':
		// Single entity
		var single DrupalEntity
		if err := json.Unmarshal(data, &single); err != nil {
			return nil, fmt.Errorf("parsing JSON object: %w", err)
		}
		entities = []DrupalEntity{single}
	default:
		return nil, fmt.Errorf("invalid JSON: expected { or [")
	}

	// Convert entities to hub records
	records := make([]*hubv1.Record, 0, len(entities))
	for i, entity := range entities {
		record, err := convertEntity(entity, opts)
		if err != nil {
			return nil, fmt.Errorf("converting entity %d: %w", i, err)
		}
		records = append(records, record)
	}

	return records, nil
}

func convertEntity(entity DrupalEntity, opts *format.ParseOptions) (*hubv1.Record, error) {
	record := &hubv1.Record{}
	// Always start from the built-in default so that field types like
	// part_detail, related_item, etc. are mapped even when a spoke-generated
	// profile (which may not enumerate every Drupal field) is active.
	// An explicit profile overrides the defaults field-by-field.
	profile := mapping.MergeProfiles(defaultProfile(), opts.Profile)

	// Track which hub fields have been set with their priorities
	priorities := make(map[string]int)

	// Process each field in the entity
	for fieldName, rawValue := range entity {
		fieldMapping, ok := profile.Fields[fieldName]
		if !ok {
			// Unknown field - might store in Extra later
			continue
		}

		// Check priority - only skip if a value was actually set at that priority.
		// Use IR+Type as the key so that fields targeting the same IR base but
		// different logical sub-types (e.g. Publication/related_item vs
		// Publication/part_detail) don't block each other.
		priorityKey := fieldMapping.IR
		if fieldMapping.Type != "" {
			priorityKey = fieldMapping.IR + "/" + fieldMapping.Type
		}
		currentPriority, hasPriority := priorities[priorityKey]
		if hasPriority && fieldMapping.Priority <= currentPriority {
			continue
		}

		// Process field based on its type and target
		// processField returns true if a value was actually set
		valueSet, err := processField(record, fieldName, rawValue, fieldMapping, opts)
		if err != nil {
			// Log error but continue processing
			continue
		}

		// Only update priority if a value was actually set
		if valueSet {
			priorities[priorityKey] = fieldMapping.Priority
		}
	}

	return record, nil
}

func processField(record *hubv1.Record, fieldName string, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	base, subfield := mapping.IRFieldName(fieldMapping.IR)

	switch base {
	case "Title":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.Title = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "AltTitle":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.AltTitle = append(record.AltTitle, cleanText(val, opts))
			return true, nil
		}
		return false, nil

	case "Abstract":
		val, _ := ExtractFormattedText(rawValue, true)
		if val != "" {
			record.Abstract = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "Description":
		val, _ := ExtractFormattedText(rawValue, true)
		if val != "" {
			record.Description = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "Contributors":
		return processContributors(record, rawValue, fieldMapping, opts)

	case "Dates":
		return processDates(record, rawValue, fieldMapping, opts)

	case "ResourceType":
		return processResourceType(record, rawValue, fieldMapping, opts)

	case "Genre":
		return processGenre(record, rawValue, fieldMapping, opts)

	case "Language":
		return processLanguage(record, rawValue, fieldMapping, opts)

	case "Rights":
		return processRights(record, rawValue, fieldMapping, opts)

	case "Subjects":
		return processSubjects(record, rawValue, fieldMapping, opts)

	case "Relations":
		return processRelations(record, rawValue, fieldMapping, opts)

	case "Publication":
		return processPublication(record, rawValue, fieldMapping, opts)

	case "Identifiers":
		return processIdentifiers(record, rawValue, fieldMapping, opts)

	case "Publisher":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.Publisher = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "PlacePublished":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.PlacePublished = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "PhysicalDesc":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.PhysicalDesc = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "Notes":
		vals, _ := ExtractStrings(rawValue)
		if len(vals) > 0 {
			for _, v := range vals {
				record.Notes = append(record.Notes, cleanText(v, opts))
			}
			return true, nil
		}
		return false, nil

	case "TableOfContents":
		val, _ := ExtractFormattedText(rawValue, true)
		if val != "" {
			record.TableOfContents = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "Source":
		val, _ := ExtractString(rawValue)
		if val != "" {
			record.Source = cleanText(val, opts)
			return true, nil
		}
		return false, nil

	case "DigitalOrigin":
		val := resolveEntityRef(rawValue, fieldMapping, opts)
		if val != "" {
			record.DigitalOrigin = val
			return true, nil
		}
		return false, nil

	case "DegreeInfo":
		return processDegreeInfo(record, subfield, rawValue, fieldMapping, opts)

	case "Extra":
		return processExtra(record, subfield, rawValue, fieldMapping, opts)
	}

	return false, nil
}

func processContributors(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	refs, err := ExtractTypedRelations(rawValue)
	if err != nil {
		return false, err
	}

	if len(refs) == 0 {
		return false, nil
	}

	for _, ref := range refs {
		contrib := &hubv1.Contributor{
			SourceId: ref.GetTargetID(),
		}

		// Get role from rel_type
		if ref.RelType != "" {
			contrib.RoleCode = ref.RelType
			contrib.Role = helpers.RelatorLabel(ref.RelType)
		}

		// Try to resolve the name from enriched data first
		if name, ok := ref.GetResolvedName(); ok {
			contrib.Name = name
			contrib.ParsedName = helpers.ParseName(name)
		} else if opts.TaxonomyResolver != nil {
			// Fall back to TaxonomyResolver
			if name, ok := opts.TaxonomyResolver.Resolve(ref.GetTargetID(), ""); ok {
				contrib.Name = name
				contrib.ParsedName = helpers.ParseName(name)
			}
		}

		// If no name resolved, use the ID
		if contrib.Name == "" {
			contrib.Name = ref.GetTargetID()
		}

		record.Contributors = append(record.Contributors, contrib)
	}

	return true, nil
}

func processDates(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	vals, _ := ExtractStrings(rawValue)

	if len(vals) == 0 {
		return false, nil
	}

	dateType := dateTypeFromString(fieldMapping.DateType)
	added := false

	for _, val := range vals {
		dateVal, err := helpers.ParseEDTF(val, dateType)
		if err != nil {
			continue
		}
		record.Dates = append(record.Dates, dateVal)
		added = true
	}

	return added, nil
}

func dateTypeFromString(s string) hubv1.DateType {
	switch strings.ToLower(s) {
	case "issued":
		return hubv1.DateType_DATE_TYPE_ISSUED
	case "created":
		return hubv1.DateType_DATE_TYPE_CREATED
	case "captured":
		return hubv1.DateType_DATE_TYPE_CAPTURED
	case "copyright":
		return hubv1.DateType_DATE_TYPE_COPYRIGHT
	case "modified":
		return hubv1.DateType_DATE_TYPE_MODIFIED
	case "available":
		return hubv1.DateType_DATE_TYPE_AVAILABLE
	case "submitted":
		return hubv1.DateType_DATE_TYPE_SUBMITTED
	case "accepted":
		return hubv1.DateType_DATE_TYPE_ACCEPTED
	case "published":
		return hubv1.DateType_DATE_TYPE_PUBLISHED
	default:
		return hubv1.DateType_DATE_TYPE_OTHER
	}
}

func processResourceType(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	refs, err := ExtractEntityRefs(rawValue)
	if err != nil {
		return false, err
	}

	// If this is a genre term carrying an authority URI, map known AAT URIs
	// directly to canonical ResourceType values.
	if len(refs) > 0 {
		ref := refs[0]
		if link, ok := ref.GetAuthorityLink(); ok {
			if rt, matched := resourceTypeFromGenreAuthorityURI(link.URI); matched {
				record.ResourceType = &hubv1.ResourceType{
					Type:       rt,
					Original:   link.URI,
					Vocabulary: link.Source,
				}
				return true, nil
			}
		}
	}

	val := resolveEntityRef(rawValue, fieldMapping, opts)
	if val != "" {
		candidate := hub.NewResourceType(val, "")
		// Avoid clobbering a specific type (e.g., inferred from genre authority URI)
		// with an unresolved/unspecified fallback value (e.g., raw taxonomy ID "11").
		if record.ResourceType != nil &&
			record.ResourceType.Type != hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED &&
			candidate.Type == hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED {
			return false, nil
		}
		record.ResourceType = candidate
		return true, nil
	}
	return false, nil
}

func processGenre(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	refs, err := ExtractEntityRefs(rawValue)
	if err != nil {
		return false, err
	}

	if len(refs) == 0 {
		return false, nil
	}

	for _, ref := range refs {
		genre := &hubv1.Subject{
			Vocabulary: hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE,
			SourceId:   ref.GetTargetID(),
		}

		// Try to get the label from enriched data
		if name, ok := ref.GetResolvedName(); ok {
			genre.Value = name
		} else if opts.TaxonomyResolver != nil {
			genre.Value, _ = opts.TaxonomyResolver.Resolve(ref.GetTargetID(), "")
		}
		if genre.Value == "" {
			genre.Value = ref.GetTargetID()
		}

		// Try to get the full authority link from enriched data (Islandora specific)
		if link, ok := ref.GetAuthorityLink(); ok {
			genre.Uri = link.URI
			// Map source to vocabulary if it's a known controlled vocabulary
			if vocab := authoritySourceToVocabulary(link.Source); vocab != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED {
				genre.Vocabulary = vocab
			}
			// Some Islandora genre AAT terms represent article-like works.
			// If no explicit resource type is set, infer one from known AAT URIs.
			if rt, matched := resourceTypeFromGenreAuthorityURI(link.URI); matched &&
				(record.ResourceType == nil || record.ResourceType.Type == hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED) {
				record.ResourceType = &hubv1.ResourceType{
					Type:       rt,
					Original:   link.URI,
					Vocabulary: link.Source,
				}
			}
		}

		record.Genres = append(record.Genres, genre)
	}

	return true, nil
}

// authoritySourceToVocabulary maps Islandora authority link source values to SubjectVocabulary.
func authoritySourceToVocabulary(source string) hubv1.SubjectVocabulary {
	switch strings.ToLower(source) {
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	case "lcsh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "lcnaf":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCNAF
	case "tgn":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GETTY_TGN
	case "mesh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func resourceTypeFromGenreAuthorityURI(uri string) (hubv1.ResourceTypeValue, bool) {
	switch normalizeAuthorityURI(uri) {
	case "http://vocab.getty.edu/page/aat/300028029",
		"http://vocab.getty.edu/page/aat/300028028",
		"http://vocab.getty.edu/page/aat/300048715":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE, true
	default:
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED, false
	}
}

func normalizeAuthorityURI(uri string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(uri)), "/")
}

// islandoraModelToResourceType maps Islandora model names to ResourceTypeValue.
// Model names come from the field_model taxonomy term name (e.g., "Collection", "Image").
func islandoraModelToResourceType(model string) hubv1.ResourceTypeValue {
	switch strings.ToLower(model) {
	case "collection":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION
	case "image":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE
	case "audio":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO
	case "video":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO
	case "digital document", "document":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_TEXT
	case "binary":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_OTHER
	case "paged content":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK
	case "publication issue":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_PERIODICAL
	case "newspaper":
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_NEWSPAPER
	default:
		return hubv1.ResourceTypeValue_RESOURCE_TYPE_UNSPECIFIED
	}
}

func processLanguage(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	val := resolveEntityRef(rawValue, fieldMapping, opts)
	if val != "" {
		record.Language = val
		return true, nil
	}
	return false, nil
}

func processRights(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	added := false
	if fieldMapping.Type == "uri" {
		// Rights as URI string
		vals, _ := ExtractStrings(rawValue)
		for _, val := range vals {
			record.Rights = append(record.Rights, hub.NewRightsFromURI(val))
			added = true
		}
	} else {
		// Rights as entity reference
		refs, err := ExtractEntityRefs(rawValue)
		if err != nil {
			return false, err
		}
		for _, ref := range refs {
			if ref.URI != "" {
				record.Rights = append(record.Rights, hub.NewRightsFromURI(ref.URI))
				added = true
			} else {
				val := ref.GetTargetID()
				// Try enriched data first
				if name, ok := ref.GetResolvedName(); ok {
					val = name
				} else if opts.TaxonomyResolver != nil {
					if resolved, ok := opts.TaxonomyResolver.Resolve(val, ""); ok {
						val = resolved
					}
				}
				record.Rights = append(record.Rights, &hubv1.Rights{Statement: val})
				added = true
			}
		}
	}
	return added, nil
}

func processSubjects(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	vocab := subjectVocabularyFromString(fieldMapping.Vocabulary)
	added := false

	if fieldMapping.Resolve != "" {
		// Entity references
		refs, err := ExtractEntityRefs(rawValue)
		if err == nil && len(refs) > 0 {
			for _, ref := range refs {
				subject := &hubv1.Subject{
					Vocabulary: vocab,
					SourceId:   ref.GetTargetID(),
				}

				// Try to get the label from enriched data
				if name, ok := ref.GetResolvedName(); ok {
					subject.Value = name
				} else if opts.TaxonomyResolver != nil {
					subject.Value, _ = opts.TaxonomyResolver.Resolve(ref.GetTargetID(), "")
				}
				if subject.Value == "" {
					subject.Value = ref.GetTargetID()
				}

				// Try to get the full authority link from enriched data (Islandora specific)
				if link, ok := ref.GetAuthorityLink(); ok {
					subject.Uri = link.URI
					// Map source to vocabulary if it provides more specific info than profile
					if authorityVocab := authoritySourceToVocabulary(link.Source); authorityVocab != hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED {
						subject.Vocabulary = authorityVocab
					}
				}

				record.Subjects = append(record.Subjects, subject)
				added = true
			}
			return added, nil
		}
	}

	// Plain text values (or fallback when resolve-mode parsing has no refs).
	vals, _ := ExtractStrings(rawValue)
	for _, val := range vals {
		record.Subjects = append(record.Subjects, &hubv1.Subject{
			Value:      cleanText(val, opts),
			Vocabulary: vocab,
		})
		added = true
	}

	return added, nil
}

func subjectVocabularyFromString(s string) hubv1.SubjectVocabulary {
	switch strings.ToLower(s) {
	case "lcsh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
	case "mesh":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
	case "aat":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_AAT
	case "fast":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_FAST
	case "ddc":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_DDC
	case "lcc":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCC
	case "keywords":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS
	case "genre":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_GENRE
	case "local":
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LOCAL
	default:
		return hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
	}
}

func processRelations(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	relType := hub.NormalizeRelationType(fieldMapping.RelationType)

	refs, err := ExtractEntityRefs(rawValue)
	if err != nil {
		return false, err
	}

	if len(refs) == 0 {
		return false, nil
	}

	for _, ref := range refs {
		rel := &hubv1.Relation{
			Type:     relType,
			SourceId: ref.GetTargetID(),
		}

		// Try enriched data first (for node titles)
		if title, ok := ref.GetResolvedName(); ok {
			rel.TargetTitle = title
		} else if fieldMapping.Resolve == "node" && opts.TaxonomyResolver != nil {
			// Fall back to TaxonomyResolver
			if title, ok := opts.TaxonomyResolver.ResolveNode(ref.GetTargetID()); ok {
				rel.TargetTitle = title
			}
		}

		if rel.TargetTitle == "" {
			rel.TargetTitle = ref.GetTargetID()
		}

		// Build the target URI from baseURL + relative path
		if opts.BaseURL != "" && ref.TargetURL != "" {
			rel.TargetUri = opts.BaseURL + ref.TargetURL
		} else if opts.BaseURL != "" && ref.GetTargetID() != "" && fieldMapping.Resolve == "node" {
			// Fallback: construct URL from node ID
			rel.TargetUri = opts.BaseURL + "/node/" + ref.GetTargetID()
		}

		// Set target ID type for nodes
		if fieldMapping.Resolve == "node" {
			rel.TargetIdType = hubv1.IdentifierType_IDENTIFIER_TYPE_NID
		}

		// Extract target resource type from enriched node data (e.g., Collection, Image)
		if model, ok := ref.GetNodeModel(); ok {
			slog.Debug("extracted node model", "targetId", ref.GetTargetID(), "model", model)
			rel.TargetResourceType = islandoraModelToResourceType(model)
		} else {
			slog.Debug("no node model found", "targetId", ref.GetTargetID(), "hasEntity", len(ref.Entity) > 0)
		}

		// Extract external type URI from model term (e.g., "https://schema.org/Collection")
		if typeURI, ok := ref.GetModelExternalURI(); ok {
			slog.Debug("extracted model external URI", "targetId", ref.GetTargetID(), "typeURI", typeURI)
			rel.TargetTypeUri = typeURI
		} else {
			slog.Debug("no model external URI found", "targetId", ref.GetTargetID())
		}

		record.Relations = append(record.Relations, rel)
	}

	return true, nil
}

func processPublication(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	if record.Publication == nil {
		record.Publication = &hubv1.PublicationDetails{}
	}

	added := false

	// Handle related_item field type
	if fieldMapping.Type == "related_item" {
		items, _ := ExtractRelatedItems(rawValue)
		for _, item := range items {
			switch item.IdentifierType {
			case "l-issn":
				// Linking ISSN for journals
				if item.Identifier != "" {
					record.Publication.LIssn = item.Identifier
					added = true
				}
				if item.Title != "" && record.Publication.Title == "" {
					record.Publication.Title = item.Title
					added = true
				}
			case "uri":
				// URI-based related item - could be a relation
				// For now, store title if available
				if item.Title != "" && record.Publication.Title == "" {
					record.Publication.Title = item.Title
					added = true
				}
			default:
				// Handle other identifier types
				if item.Title != "" && record.Publication.Title == "" {
					record.Publication.Title = item.Title
					added = true
				}
			}
		}
		return added, nil
	}

	// Handle part_detail field type.
	// Types mirror MODS <detail type="...">: volume, issue, page, chapter,
	// section, heading, illustration, article.
	if fieldMapping.Type == "part_detail" {
		parts, _ := ExtractPartDetails(rawValue)
		for _, part := range parts {
			switch part.Type {
			case "volume":
				// Number holds the actual volume value; Caption is a display
				// label ("Vol.") and should not be stored as the volume number.
				if part.Number != "" {
					record.Publication.Volume = part.Number
					added = true
				}
			case "issue":
				// Same reasoning as volume — Caption is a label, not a value.
				if part.Number != "" {
					record.Publication.Issue = part.Number
					added = true
				}
			case "page":
				// Number already encodes the full page range (e.g. "1-15").
				// Two separate page entries do not mean start/end pages.
				if part.Number != "" && record.Publication.Pages == "" {
					record.Publication.Pages = part.Number
					added = true
				}
			case "chapter":
				if part.Number != "" {
					hub.SetExtra(record, "chapter_number", part.Number)
					added = true
				}
				if part.Title != "" {
					hub.SetExtra(record, "chapter_title", part.Title)
					added = true
				}
			case "section":
				// Section info belongs in extra — Publication.Title is the
				// container/journal title, not a section heading.
				if part.Number != "" {
					hub.SetExtra(record, "section_number", part.Number)
					added = true
				}
				if part.Title != "" {
					hub.SetExtra(record, "section_title", part.Title)
					added = true
				}
			case "heading":
				// Heading text lives in Title per the MODS <detail type="heading"> convention.
				if part.Title != "" {
					hub.SetExtra(record, "part_heading", part.Title)
					added = true
				}
			case "illustration":
				// Illustrations are descriptive; Caption is the primary carrier.
				if part.Caption != "" {
					hub.SetExtra(record, "part_illustration", part.Caption)
					added = true
				} else if part.Title != "" {
					hub.SetExtra(record, "part_illustration", part.Title)
					added = true
				}
			case "article":
				// An article number is an electronic locator (e.g. e12345),
				// not a page range.  Store separately so downstream serializers
				// can choose the right output field (e.g. CrossRef articleNumber).
				if part.Number != "" {
					hub.SetExtra(record, "article_number", part.Number)
					added = true
				}
			default:
				// Preserve unrecognised part types in extra so no data is lost.
				if part.Number != "" {
					hub.SetExtra(record, "part_"+part.Type+"_number", part.Number)
					added = true
				}
				if part.Title != "" {
					hub.SetExtra(record, "part_"+part.Type+"_title", part.Title)
					added = true
				}
			}
		}
		return added, nil
	}

	return false, nil
}

func processIdentifiers(record *hubv1.Record, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	// Handle textfield_attr and textarea_attr field types
	if fieldMapping.Type == "textfield_attr" || fieldMapping.Type == "textarea_attr" {
		attrFields, _ := ExtractAttrFields(rawValue)
		if len(attrFields) == 0 {
			return false, nil
		}
		for _, field := range attrFields {
			idType := hub.DetectIdentifierType(field.Value)
			// Use attr0 to determine identifier type if present
			if field.Attr0 != "" {
				idType = identifierTypeFromString(field.Attr0)
			}
			record.Identifiers = append(record.Identifiers, hub.NewIdentifier(field.Value, idType))
		}
		return true, nil
	}

	// Default: extract as plain strings
	vals, _ := ExtractStrings(rawValue)
	if len(vals) == 0 {
		return false, nil
	}
	for _, val := range vals {
		idType := hub.DetectIdentifierType(val)
		if fieldMapping.Type != "" {
			idType = identifierTypeFromString(fieldMapping.Type)
		}
		record.Identifiers = append(record.Identifiers, hub.NewIdentifier(val, idType))
	}
	return true, nil
}

func identifierTypeFromString(s string) hubv1.IdentifierType {
	switch strings.ToLower(s) {
	case "doi":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "url", "uri":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	case "handle":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "isbn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "issn", "l-issn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "orcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	case "pmid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMID
	case "pmcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID
	case "arxiv":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV
	case "local", "islandora", "item-number", "file-name", "barcode":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	case "pid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_PID
	case "nid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_NID
	case "uuid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UUID
	case "isni":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISNI
	case "report-number":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_REPORT_NUMBER
	case "call-number":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_CALL_NUMBER
	case "oclc", "reference":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED
	}
}

func processDegreeInfo(record *hubv1.Record, subfield string, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	var val string
	if fieldMapping.Resolve != "" {
		val = resolveEntityRef(rawValue, fieldMapping, opts)
	} else {
		val, _ = ExtractString(rawValue)
	}

	if val == "" {
		return false, nil
	}

	if record.DegreeInfo == nil {
		record.DegreeInfo = &hubv1.DegreeInfo{}
	}

	switch subfield {
	case "DegreeName":
		record.DegreeInfo.DegreeName = val
	case "DegreeLevel":
		record.DegreeInfo.DegreeLevel = val
	case "Department":
		record.DegreeInfo.Department = val
	case "Institution":
		record.DegreeInfo.Institution = val
	}

	return true, nil
}

func processExtra(record *hubv1.Record, subfield string, rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) (bool, error) {
	// Try to extract as various types
	if val, err := ExtractString(rawValue); err == nil && val != "" {
		hub.SetExtra(record, subfield, val)
		return true, nil
	}

	if val, err := ExtractInt(rawValue); err == nil && val != 0 {
		hub.SetExtra(record, subfield, val)
		return true, nil
	}

	if val, err := ExtractBool(rawValue); err == nil {
		hub.SetExtra(record, subfield, val)
		return true, nil
	}

	// Store raw value
	var generic any
	if err := json.Unmarshal(rawValue, &generic); err == nil {
		hub.SetExtra(record, subfield, generic)
		return true, nil
	}

	return false, nil
}

// Helper functions

func resolveEntityRef(rawValue json.RawMessage, fieldMapping mapping.FieldMapping, opts *format.ParseOptions) string {
	refs, err := ExtractEntityRefs(rawValue)
	if err != nil || len(refs) == 0 {
		return ""
	}

	ref := refs[0]
	targetID := ref.GetTargetID()

	// Try enriched data first
	if val, ok := ref.GetResolvedName(); ok {
		return val
	}

	// Fall back to TaxonomyResolver
	if opts.TaxonomyResolver != nil && fieldMapping.Resolve == "taxonomy_term" {
		if val, ok := opts.TaxonomyResolver.Resolve(targetID, ""); ok {
			return val
		}
	}

	if opts.TaxonomyResolver != nil && fieldMapping.Resolve == "node" {
		if val, ok := opts.TaxonomyResolver.ResolveNode(targetID); ok {
			return val
		}
	}

	return targetID
}

func cleanText(s string, opts *format.ParseOptions) string {
	if opts.StripHTML {
		return helpers.CleanText(s)
	}
	return strings.TrimSpace(s)
}

func trimBOM(data []byte) []byte {
	// UTF-8 BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

func skipWhitespace(data []byte) []byte {
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\n' || data[0] == '\r') {
		data = data[1:]
	}
	return data
}

func defaultProfile() *mapping.Profile {
	return &mapping.Profile{
		Name:   "default",
		Format: "drupal",
		Fields: map[string]mapping.FieldMapping{
			"title":                   {IR: "Title"},
			"field_full_title":        {IR: "Title", Priority: 1},
			"field_alt_title":         {IR: "AltTitle"},
			"field_abstract":          {IR: "Abstract"},
			"field_description":       {IR: "Description"},
			"field_linked_agent":      {IR: "Contributors", Type: "typed_relation", RoleField: "rel_type", Resolve: "taxonomy_term"},
			"field_edtf_date_issued":  {IR: "Dates", DateType: "issued", Parser: "edtf"},
			"field_edtf_date_created": {IR: "Dates", DateType: "created", Parser: "edtf"},
			"field_resource_type":     {IR: "ResourceType", Resolve: "taxonomy_term"},
			"field_genre":             {IR: "Genre", Resolve: "taxonomy_term"},
			"field_language":          {IR: "Language", Resolve: "taxonomy_term"},
			"field_rights":            {IR: "Rights", Type: "uri"},
			"field_subject":           {IR: "Subjects", Resolve: "taxonomy_term"},
			"field_lcsh_topic":        {IR: "Subjects", Resolve: "taxonomy_term", Vocabulary: "lcsh"},
			"field_keywords":          {IR: "Subjects", Resolve: "taxonomy_term", Vocabulary: "keywords"},
			"field_publisher":         {IR: "Publisher"},
			"field_place_published":   {IR: "PlacePublished"},
			"field_member_of":         {IR: "Relations", RelationType: "member_of", Resolve: "node"},
			"field_related_item":      {IR: "Publication", Type: "related_item"},
			"field_part_detail":       {IR: "Publication", Type: "part_detail"},
			"field_identifier":        {IR: "Identifiers", Type: "textfield_attr"},
			"field_note":              {IR: "Notes"},
			"field_degree_name":       {IR: "DegreeInfo.DegreeName"},
			"field_degree_level":      {IR: "DegreeInfo.DegreeLevel"},
			"field_department_name":   {IR: "DegreeInfo.Department", Resolve: "taxonomy_term"},
			"nid":                     {IR: "Extra.nid"},
			"uuid":                    {IR: "Extra.uuid"},
			"created":                 {IR: "Extra.created"},
			"changed":                 {IR: "Extra.changed"},
			"status":                  {IR: "Extra.status"},
			"type":                    {IR: "Extra.type"},
		},
	}
}
