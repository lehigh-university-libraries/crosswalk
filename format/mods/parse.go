package mods

import (
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	"github.com/lehigh-university-libraries/crosswalk/format"
	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	modsv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/mods/v3_8"
)

// Parse reads MODS XML and returns hub records.
// Handles both bare <mods> elements and <modsCollection> wrappers containing
// multiple <mods> elements. The protoxml unmarshaler scans for all <mods>
// elements regardless of nesting depth, so both cases are handled uniformly.
func (f *Format) Parse(r io.Reader, _ *format.ParseOptions) ([]*hubv1.Record, error) {
	spokeMessages, err := protoxml.UnmarshalAll(r, func() proto.Message {
		return &modsv1.Record{}
	})
	if err != nil {
		return nil, fmt.Errorf("parsing MODS XML: %w", err)
	}

	if len(spokeMessages) == 0 {
		return nil, fmt.Errorf("no <mods> elements found in input")
	}

	records := make([]*hubv1.Record, 0, len(spokeMessages))

	for i, msg := range spokeMessages {
		spoke, ok := msg.(*modsv1.Record)
		if !ok {
			return nil, fmt.Errorf("record %d: unexpected message type %T", i, msg)
		}

		record := spokeToHub(spoke)
		record.SourceInfo = &hubv1.SourceInfo{
			Format:        "mods",
			FormatVersion: Version,
		}

		records = append(records, record)
	}

	return records, nil
}

// spokeToHub converts a MODS spoke record to a hub record.
func spokeToHub(spoke *modsv1.Record) *hubv1.Record {
	record := &hubv1.Record{}

	// Title: use the first titleInfo with no type as the primary title.
	for _, ti := range spoke.TitleInfo {
		if ti.Type == modsv1.TitleType_TITLE_TYPE_UNSPECIFIED && record.Title == "" {
			record.Title = ti.Title
		} else if ti.Type == modsv1.TitleType_TITLE_TYPE_ALTERNATIVE {
			record.AltTitle = append(record.AltTitle, ti.Title)
		}
	}

	// Contributors from name elements.
	for _, name := range spoke.Name {
		contributor := nameToContributor(name)
		if contributor != nil {
			record.Contributors = append(record.Contributors, contributor)
		}
	}

	// Resource type from typeOfResource.
	if len(spoke.TypeOfResource) > 0 {
		record.ResourceType = &hubv1.ResourceType{
			Original: spoke.TypeOfResource[0].Value,
		}
	}

	// Origin info: publisher, place, dates.
	for _, oi := range spoke.OriginInfo {
		if len(oi.Publisher) > 0 && record.Publisher == "" {
			record.Publisher = oi.Publisher[0]
		}
		for _, place := range oi.Place {
			for _, pt := range place.PlaceTerm {
				if pt.Value != "" && record.PlacePublished == "" {
					record.PlacePublished = pt.Value
				}
			}
		}
		for _, d := range oi.DateIssued {
			if d.Value != "" {
				record.Dates = append(record.Dates, &hubv1.DateValue{
					Type: hubv1.DateType_DATE_TYPE_ISSUED,
					Raw:  d.Value,
				})
			}
		}
		for _, d := range oi.DateCreated {
			if d.Value != "" {
				record.Dates = append(record.Dates, &hubv1.DateValue{
					Type: hubv1.DateType_DATE_TYPE_CREATED,
					Raw:  d.Value,
				})
			}
		}
		for _, d := range oi.CopyrightDate {
			if d.Value != "" {
				record.Dates = append(record.Dates, &hubv1.DateValue{
					Type: hubv1.DateType_DATE_TYPE_COPYRIGHT,
					Raw:  d.Value,
				})
			}
		}
		for _, d := range oi.DateModified {
			if d.Value != "" {
				record.Dates = append(record.Dates, &hubv1.DateValue{
					Type: hubv1.DateType_DATE_TYPE_MODIFIED,
					Raw:  d.Value,
				})
			}
		}
	}

	// Abstract: use the first abstract value.
	for _, a := range spoke.Abstract {
		if a.Value != "" && record.Abstract == "" {
			record.Abstract = a.Value
		}
	}

	// Language: use the first language term value.
	for _, lang := range spoke.Language {
		for _, lt := range lang.LanguageTerm {
			if lt.Value != "" && record.Language == "" {
				record.Language = lt.Value
			}
		}
	}

	// Subjects from subject elements.
	for _, subj := range spoke.Subject {
		vocab := hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_UNSPECIFIED
		switch subj.Authority {
		case "lcsh":
			vocab = hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_LCSH
		case "mesh":
			vocab = hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_MESH
		}
		for _, topic := range subj.Topic {
			record.Subjects = append(record.Subjects, &hubv1.Subject{
				Value:      topic,
				Vocabulary: vocab,
			})
		}
	}

	// Genre elements mapped to genres.
	for _, g := range spoke.Genre {
		if g.Value != "" {
			record.Genres = append(record.Genres, &hubv1.Subject{
				Value: g.Value,
			})
		}
	}

	// Identifiers.
	for _, id := range spoke.Identifier {
		if id.Value != "" {
			record.Identifiers = append(record.Identifiers, &hubv1.Identifier{
				Type:  mapIdentifierType(id.Type),
				Value: id.Value,
			})
		}
	}

	// Notes.
	for _, n := range spoke.Note {
		if n.Value != "" {
			record.Notes = append(record.Notes, n.Value)
		}
	}

	// Related items.
	for _, ri := range spoke.RelatedItem {
		rel := &hubv1.Relation{
			Type: mapRelationType(ri.Type),
		}
		if ri.Record != nil {
			for _, ti := range ri.Record.TitleInfo {
				if ti.Title != "" && rel.TargetTitle == "" {
					rel.TargetTitle = ti.Title
				}
			}
		}
		if rel.TargetTitle != "" {
			record.Relations = append(record.Relations, rel)
		}
	}

	// Access conditions mapped to rights.
	for _, ac := range spoke.AccessCondition {
		rights := &hubv1.Rights{}
		if ac.Value != "" {
			rights.Statement = ac.Value
		}
		if ac.Href != "" {
			rights.Uri = ac.Href
		}
		if rights.Statement != "" || rights.Uri != "" {
			record.Rights = append(record.Rights, rights)
		}
	}

	return record
}

// nameToContributor converts a MODS Name to a hub Contributor.
func nameToContributor(name *modsv1.Name) *hubv1.Contributor {
	c := &hubv1.Contributor{}

	// Contributor type from name type.
	switch name.Type {
	case modsv1.NameType_NAME_TYPE_PERSONAL, modsv1.NameType_NAME_TYPE_FAMILY:
		c.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_PERSON
	case modsv1.NameType_NAME_TYPE_CORPORATE, modsv1.NameType_NAME_TYPE_CONFERENCE:
		c.Type = hubv1.ContributorType_CONTRIBUTOR_TYPE_ORGANIZATION
	}

	// Parse name parts.
	var given, family string
	var untyped []string
	for _, np := range name.NamePart {
		switch np.Type {
		case modsv1.NamePartType_NAME_PART_TYPE_GIVEN:
			given = np.Value
		case modsv1.NamePartType_NAME_PART_TYPE_FAMILY:
			family = np.Value
		default:
			untyped = append(untyped, np.Value)
		}
	}

	if given != "" || family != "" {
		c.ParsedName = &hubv1.ParsedName{
			Given:  given,
			Family: family,
		}
		if family != "" && given != "" {
			c.Name = family + ", " + given
		} else if family != "" {
			c.Name = family
		} else {
			c.Name = given
		}
	} else if len(untyped) > 0 {
		c.Name = untyped[0]
	}

	// Display form overrides constructed name.
	if name.DisplayForm != "" {
		c.Name = name.DisplayForm
	}

	// Role from the first role term.
	for _, role := range name.Role {
		for _, rt := range role.RoleTerm {
			if rt.Value != "" && c.Role == "" {
				c.Role = rt.Value
			}
		}
	}

	// Affiliations.
	for _, aff := range name.Affiliation {
		if aff != "" {
			if c.Affiliation == "" {
				c.Affiliation = aff
			}
			c.Affiliations = append(c.Affiliations, &hubv1.Affiliation{
				Name: aff,
			})
		}
	}

	if c.Name == "" {
		return nil
	}

	return c
}

// mapIdentifierType maps a MODS identifier type string to a hub IdentifierType.
func mapIdentifierType(modsType string) hubv1.IdentifierType {
	switch modsType {
	case "doi":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_DOI
	case "isbn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN
	case "issn":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN
	case "uri", "url":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_URL
	case "hdl":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_HANDLE
	case "orcid":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID
	case "local":
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	default:
		return hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL
	}
}

// mapRelationType maps a MODS related item type to a hub RelationType.
func mapRelationType(rt modsv1.RelatedItemType) hubv1.RelationType {
	switch rt {
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_HOST:
		return hubv1.RelationType_RELATION_TYPE_PART_OF
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_CONSTITUENT:
		return hubv1.RelationType_RELATION_TYPE_HAS_PART
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_SERIES:
		return hubv1.RelationType_RELATION_TYPE_IN_SERIES
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_PRECEDING:
		return hubv1.RelationType_RELATION_TYPE_REPLACES
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_SUCCEEDING:
		return hubv1.RelationType_RELATION_TYPE_IS_REPLACED_BY
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_ORIGINAL:
		return hubv1.RelationType_RELATION_TYPE_VERSION_OF
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_OTHER_VERSION:
		return hubv1.RelationType_RELATION_TYPE_HAS_VERSION
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_IS_REFERENCED_BY:
		return hubv1.RelationType_RELATION_TYPE_IS_CITED_BY
	case modsv1.RelatedItemType_RELATED_ITEM_TYPE_REFERENCES:
		return hubv1.RelationType_RELATION_TYPE_CITES
	default:
		return hubv1.RelationType_RELATION_TYPE_OTHER
	}
}
