package csl

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	cslv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/csl/v1"
	"github.com/lehigh-university-libraries/crosswalk/helpers"
)

// Serialize writes hub records as CSL-JSON.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	if opts == nil {
		opts = format.NewSerializeOptions()
	}

	items := make([]JSONItem, 0, len(records))
	for i, record := range records {
		spokeItem, err := hubToSpoke(record)
		if err != nil {
			return fmt.Errorf("converting record %d to spoke: %w", i, err)
		}
		items = append(items, spokeToJSON(spokeItem))
	}

	encoder := json.NewEncoder(w)
	if opts.Pretty {
		encoder.SetIndent("", "  ")
	}

	if len(items) == 1 {
		return encoder.Encode(items[0])
	}
	return encoder.Encode(items)
}

// hubToSpoke converts a hub record to the CSL spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*cslv1.Item, error) {
	item := &cslv1.Item{
		Title:    record.Title,
		Abstract: record.Abstract,
		Language: record.Language,
	}

	// ID from identifiers
	for _, id := range record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
			if item.Id == "" {
				item.Id = id.Value
			}
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			item.Doi = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
			item.Url = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
			item.Isbn = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
			item.Issn = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_PMID:
			item.Pmid = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_PMCID:
			item.Pmcid = id.Value
		}
	}

	// Generate ID if not found
	if item.Id == "" {
		item.Id = generateID(record)
	}

	// Type from resource type
	item.Type = mapResourceTypeToCSL(record.ResourceType)

	// Authors, editors, translators from contributors
	for _, c := range record.Contributors {
		name := &cslv1.Name{}
		if c.ParsedName != nil {
			// CSL "given" covers all given names including middle initials.
			name.Given = c.ParsedName.Given
			if c.ParsedName.Middle != "" {
				name.Given = strings.TrimSpace(name.Given + " " + c.ParsedName.Middle)
			}
			name.Family = c.ParsedName.Family
			name.Suffix = c.ParsedName.Suffix
		} else if c.Name != "" {
			name.Literal = c.Name
		}

		// Prefer RoleCode (e.g., relators:ths) and normalize to MARC relator code.
		switch contributorRoleCode(c) {
		case "edt":
			item.Editor = append(item.Editor, name)
		case "trl":
			item.Translator = append(item.Translator, name)
		case "aut", "cre", "":
			// Empty role is treated as author for backward compatibility.
			item.Author = append(item.Author, name)
		default:
			// Non-authorial contributors (e.g., thesis advisor "ths") do not
			// map to CSL author/editor/translator name lists.
		}
	}

	// Publisher
	item.Publisher = record.Publisher
	item.PublisherPlace = record.PlacePublished

	// Container title from relations
	for _, rel := range record.Relations {
		if rel.Type == hubv1.RelationType_RELATION_TYPE_PART_OF {
			item.ContainerTitle = rel.TargetTitle
			break
		}
	}

	// Dates
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			item.Issued = &cslv1.Date{
				DateParts: []*cslv1.DateParts{{
					Year:  d.Year,
					Month: d.Month,
					Day:   d.Day,
				}},
			}
			break
		}
	}

	// Publication details (volume, issue, pages, container title)
	if record.Publication != nil {
		item.Volume = record.Publication.Volume
		item.Issue = record.Publication.Issue
		item.Page = record.Publication.Pages
		if record.Publication.Issn != "" {
			item.Issn = record.Publication.Issn
		}
		// Publication.Title is the container/journal title; prefer a
		// PART_OF relation title (set above) if both are present.
		if item.ContainerTitle == "" && record.Publication.Title != "" {
			item.ContainerTitle = record.Publication.Title
		}
	}

	// Edition
	item.Edition = record.Edition

	// Notes
	if len(record.Notes) > 0 {
		item.Note = strings.Join(record.Notes, "; ")
	}

	return item, nil
}

// mapResourceTypeToCSL maps hub resource type to CSL item type.
func mapResourceTypeToCSL(rt *hubv1.ResourceType) cslv1.ItemType {
	if rt == nil {
		return cslv1.ItemType_ITEM_TYPE_DOCUMENT
	}

	switch rt.Type {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER:
		return cslv1.ItemType_ITEM_TYPE_ARTICLE_JOURNAL
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK:
		return cslv1.ItemType_ITEM_TYPE_BOOK
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER:
		return cslv1.ItemType_ITEM_TYPE_CHAPTER
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER:
		return cslv1.ItemType_ITEM_TYPE_PAPER_CONFERENCE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS:
		return cslv1.ItemType_ITEM_TYPE_THESIS
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION:
		return cslv1.ItemType_ITEM_TYPE_THESIS
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT:
		return cslv1.ItemType_ITEM_TYPE_REPORT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return cslv1.ItemType_ITEM_TYPE_DATASET
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return cslv1.ItemType_ITEM_TYPE_SOFTWARE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE:
		return cslv1.ItemType_ITEM_TYPE_WEBPAGE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_PATENT:
		return cslv1.ItemType_ITEM_TYPE_PATENT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_IMAGE:
		return cslv1.ItemType_ITEM_TYPE_GRAPHIC
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_VIDEO:
		return cslv1.ItemType_ITEM_TYPE_MOTION_PICTURE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_AUDIO:
		return cslv1.ItemType_ITEM_TYPE_SONG
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT:
		return cslv1.ItemType_ITEM_TYPE_MANUSCRIPT
	default:
		return cslv1.ItemType_ITEM_TYPE_DOCUMENT
	}
}

func contributorRoleCode(c *hubv1.Contributor) string {
	if c == nil {
		return ""
	}
	if code := helpers.NormalizeRole(c.RoleCode); code != "" {
		return code
	}
	return helpers.NormalizeRole(c.Role)
}

// generateID creates an ID from record metadata.
func generateID(record *hubv1.Record) string {
	var author string
	if len(record.Contributors) > 0 {
		c := record.Contributors[0]
		if c.ParsedName != nil && c.ParsedName.Family != "" {
			author = c.ParsedName.Family
		} else if c.Name != "" {
			parts := strings.Fields(c.Name)
			if len(parts) > 0 {
				author = parts[len(parts)-1]
			}
		}
	}
	if author == "" {
		author = "unknown"
	}

	var year string
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			if d.Year > 0 {
				year = fmt.Sprintf("%d", d.Year)
				break
			}
		}
	}
	if year == "" {
		year = "nd"
	}

	author = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, author)

	return strings.ToLower(author) + year
}

// spokeToJSON converts a spoke proto struct to JSON-serializable struct.
func spokeToJSON(spoke *cslv1.Item) JSONItem {
	item := JSONItem{
		ID:             spoke.Id,
		Type:           itemTypeToString(spoke.Type),
		Title:          spoke.Title,
		Abstract:       spoke.Abstract,
		Language:       spoke.Language,
		DOI:            spoke.Doi,
		URL:            spoke.Url,
		ISBN:           spoke.Isbn,
		ISSN:           spoke.Issn,
		PMID:           spoke.Pmid,
		PMCID:          spoke.Pmcid,
		Publisher:      spoke.Publisher,
		PublisherPlace: spoke.PublisherPlace,
		ContainerTitle: spoke.ContainerTitle,
		Edition:        spoke.Edition,
		Note:           spoke.Note,
		Volume:         spoke.Volume,
		Issue:          spoke.Issue,
		Page:           spoke.Page,
	}

	// Authors
	for _, a := range spoke.Author {
		item.Author = append(item.Author, nameToJSON(a))
	}
	for _, e := range spoke.Editor {
		item.Editor = append(item.Editor, nameToJSON(e))
	}
	for _, t := range spoke.Translator {
		item.Translator = append(item.Translator, nameToJSON(t))
	}

	// Issued date
	if spoke.Issued != nil && len(spoke.Issued.DateParts) > 0 {
		dp := spoke.Issued.DateParts[0]
		var parts []int
		if dp.Year > 0 {
			parts = append(parts, int(dp.Year))
		}
		if dp.Month > 0 {
			parts = append(parts, int(dp.Month))
		}
		if dp.Day > 0 {
			parts = append(parts, int(dp.Day))
		}
		if len(parts) > 0 {
			item.Issued = &JSONDate{DateParts: [][]int{parts}}
		}
	}

	return item
}

func nameToJSON(n *cslv1.Name) JSONName {
	return JSONName{
		Family:  n.Family,
		Given:   n.Given,
		Suffix:  n.Suffix,
		Literal: n.Literal,
	}
}

func itemTypeToString(t cslv1.ItemType) string {
	switch t {
	case cslv1.ItemType_ITEM_TYPE_ARTICLE_JOURNAL:
		return "article-journal"
	case cslv1.ItemType_ITEM_TYPE_BOOK:
		return "book"
	case cslv1.ItemType_ITEM_TYPE_CHAPTER:
		return "chapter"
	case cslv1.ItemType_ITEM_TYPE_PAPER_CONFERENCE:
		return "paper-conference"
	case cslv1.ItemType_ITEM_TYPE_THESIS:
		return "thesis"
	case cslv1.ItemType_ITEM_TYPE_REPORT:
		return "report"
	case cslv1.ItemType_ITEM_TYPE_DATASET:
		return "dataset"
	case cslv1.ItemType_ITEM_TYPE_SOFTWARE:
		return "software"
	case cslv1.ItemType_ITEM_TYPE_WEBPAGE:
		return "webpage"
	case cslv1.ItemType_ITEM_TYPE_PATENT:
		return "patent"
	case cslv1.ItemType_ITEM_TYPE_GRAPHIC:
		return "graphic"
	case cslv1.ItemType_ITEM_TYPE_MOTION_PICTURE:
		return "motion_picture"
	case cslv1.ItemType_ITEM_TYPE_SONG:
		return "song"
	case cslv1.ItemType_ITEM_TYPE_MANUSCRIPT:
		return "manuscript"
	default:
		return "document"
	}
}

// JSON types for CSL-JSON output.

type JSONItem struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Title          string     `json:"title,omitempty"`
	Abstract       string     `json:"abstract,omitempty"`
	Language       string     `json:"language,omitempty"`
	Author         []JSONName `json:"author,omitempty"`
	Editor         []JSONName `json:"editor,omitempty"`
	Translator     []JSONName `json:"translator,omitempty"`
	Issued         *JSONDate  `json:"issued,omitempty"`
	DOI            string     `json:"DOI,omitempty"`
	URL            string     `json:"URL,omitempty"`
	ISBN           string     `json:"ISBN,omitempty"`
	ISSN           string     `json:"ISSN,omitempty"`
	PMID           string     `json:"PMID,omitempty"`
	PMCID          string     `json:"PMCID,omitempty"`
	Publisher      string     `json:"publisher,omitempty"`
	PublisherPlace string     `json:"publisher-place,omitempty"`
	ContainerTitle string     `json:"container-title,omitempty"`
	Edition        string     `json:"edition,omitempty"`
	Volume         string     `json:"volume,omitempty"`
	Issue          string     `json:"issue,omitempty"`
	Page           string     `json:"page,omitempty"`
	Note           string     `json:"note,omitempty"`
}

type JSONName struct {
	Family  string `json:"family,omitempty"`
	Given   string `json:"given,omitempty"`
	Suffix  string `json:"suffix,omitempty"`
	Literal string `json:"literal,omitempty"`
}

type JSONDate struct {
	DateParts [][]int `json:"date-parts,omitempty"`
}
