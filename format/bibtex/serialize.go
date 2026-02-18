package bibtex

import (
	"fmt"
	"io"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
	bibtexv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/bibtex/v1"
)

// Serialize writes hub records as BibTeX entries.
func (f *Format) Serialize(w io.Writer, records []*hubv1.Record, opts *format.SerializeOptions) error {
	// opts reserved for future use (e.g., encoding options)
	_ = opts

	for i, record := range records {
		// Step 1: Convert hub record to spoke proto struct
		spokeEntry, err := hubToSpoke(record)
		if err != nil {
			return fmt.Errorf("converting record %d to spoke: %w", i, err)
		}

		// Step 2: Serialize spoke struct to BibTeX text
		bibtexText := spokeToBibtex(spokeEntry)

		if _, err := w.Write([]byte(bibtexText)); err != nil {
			return err
		}
		if i < len(records)-1 {
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}

	return nil
}

// hubToSpoke converts a hub record to the BibTeX spoke proto struct.
func hubToSpoke(record *hubv1.Record) (*bibtexv1.Entry, error) {
	entry := &bibtexv1.Entry{
		Title:    record.Title,
		Abstract: record.Abstract,
	}

	// Entry type from resource type
	entry.EntryType = mapResourceTypeToBibtex(record.ResourceType)

	// Citation key from identifiers
	for _, id := range record.Identifiers {
		switch id.Type {
		case hubv1.IdentifierType_IDENTIFIER_TYPE_LOCAL:
			if entry.CitationKey == "" {
				entry.CitationKey = id.Value
			}
		case hubv1.IdentifierType_IDENTIFIER_TYPE_DOI:
			entry.Doi = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_URL:
			entry.Url = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISBN:
			entry.Isbn = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ISSN:
			entry.Issn = id.Value
		case hubv1.IdentifierType_IDENTIFIER_TYPE_ARXIV:
			entry.Eprint = id.Value
			entry.Eprinttype = "arxiv"
		}
	}

	// Generate citation key if not found
	if entry.CitationKey == "" {
		entry.CitationKey = generateCitationKey(record)
	}

	// Authors and editors from contributors
	for _, c := range record.Contributors {
		person := &bibtexv1.Person{}
		if c.ParsedName != nil {
			person.Given = c.ParsedName.Given
			person.Family = c.ParsedName.Family
			person.Suffix = c.ParsedName.Suffix
		}
		if person.Family == "" && c.Name != "" {
			person.Name = c.Name
		}

		// Check for ORCID
		for _, cid := range c.Identifiers {
			if cid.Type == hubv1.IdentifierType_IDENTIFIER_TYPE_ORCID {
				person.Orcid = cid.Value
			}
		}

		role := strings.ToLower(c.Role)
		if role == "editor" || role == "edt" {
			entry.Editor = append(entry.Editor, person)
		} else {
			entry.Author = append(entry.Author, person)
		}
	}

	// Publisher
	entry.Publisher = record.Publisher

	// Place published
	entry.Address = record.PlacePublished

	// Edition
	entry.Edition = record.Edition

	// Language
	entry.Language = record.Language

	// Dates
	for _, d := range record.Dates {
		if d.Type == hubv1.DateType_DATE_TYPE_ISSUED || d.Type == hubv1.DateType_DATE_TYPE_PUBLISHED {
			if d.Year > 0 {
				entry.Year = fmt.Sprintf("%d", d.Year)
			}
			if d.Month > 0 {
				entry.Month = monthToString(int(d.Month))
			}
			break
		}
	}

	// Relations (journal, booktitle, series)
	for _, rel := range record.Relations {
		switch rel.Type {
		case hubv1.RelationType_RELATION_TYPE_PART_OF:
			// Determine if it's a journal or book based on entry type
			if entry.EntryType == bibtexv1.EntryType_ENTRY_TYPE_ARTICLE {
				entry.Journal = rel.TargetTitle
			} else {
				entry.Booktitle = rel.TargetTitle
			}
		case hubv1.RelationType_RELATION_TYPE_IN_SERIES:
			entry.Series = rel.TargetTitle
		}
	}

	// Degree info for theses
	if record.DegreeInfo != nil {
		entry.School = record.DegreeInfo.Institution
		if record.DegreeInfo.DegreeName != "" {
			entry.Type = record.DegreeInfo.DegreeName
		}
	}

	// Keywords from subjects
	for _, s := range record.Subjects {
		if s.Vocabulary == hubv1.SubjectVocabulary_SUBJECT_VOCABULARY_KEYWORDS {
			entry.Keywords = append(entry.Keywords, s.Value)
		}
	}

	// Notes
	if len(record.Notes) > 0 {
		entry.Note = strings.Join(record.Notes, "; ")
	}

	return entry, nil
}

// mapResourceTypeToBibtex maps hub resource type to BibTeX entry type.
func mapResourceTypeToBibtex(rt *hubv1.ResourceType) bibtexv1.EntryType {
	if rt == nil {
		return bibtexv1.EntryType_ENTRY_TYPE_MISC
	}

	switch rt.Type {
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_ARTICLE,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_PREPRINT,
		hubv1.ResourceTypeValue_RESOURCE_TYPE_WORKING_PAPER:
		return bibtexv1.EntryType_ENTRY_TYPE_ARTICLE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK:
		return bibtexv1.EntryType_ENTRY_TYPE_BOOK
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_BOOK_CHAPTER:
		return bibtexv1.EntryType_ENTRY_TYPE_INCOLLECTION
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_CONFERENCE_PAPER:
		return bibtexv1.EntryType_ENTRY_TYPE_INPROCEEDINGS
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_THESIS:
		return bibtexv1.EntryType_ENTRY_TYPE_MASTERSTHESIS
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DISSERTATION:
		return bibtexv1.EntryType_ENTRY_TYPE_PHDTHESIS
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_TECHNICAL_REPORT:
		return bibtexv1.EntryType_ENTRY_TYPE_TECHREPORT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_REPORT:
		return bibtexv1.EntryType_ENTRY_TYPE_REPORT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_DATASET:
		return bibtexv1.EntryType_ENTRY_TYPE_DATASET
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_SOFTWARE:
		return bibtexv1.EntryType_ENTRY_TYPE_SOFTWARE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_PATENT:
		return bibtexv1.EntryType_ENTRY_TYPE_PATENT
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_WEBPAGE:
		return bibtexv1.EntryType_ENTRY_TYPE_ONLINE
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_MANUSCRIPT:
		return bibtexv1.EntryType_ENTRY_TYPE_UNPUBLISHED
	case hubv1.ResourceTypeValue_RESOURCE_TYPE_COLLECTION:
		return bibtexv1.EntryType_ENTRY_TYPE_PROCEEDINGS
	default:
		return bibtexv1.EntryType_ENTRY_TYPE_MISC
	}
}

// generateCitationKey creates a citation key from record metadata.
func generateCitationKey(record *hubv1.Record) string {
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

	// Clean author name
	author = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, author)

	return strings.ToLower(author) + year
}

// monthToString converts month number to BibTeX month abbreviation.
func monthToString(month int) string {
	months := []string{"", "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
	if month >= 1 && month <= 12 {
		return months[month]
	}
	return ""
}

// spokeToBibtex converts a spoke proto struct to BibTeX text.
func spokeToBibtex(entry *bibtexv1.Entry) string {
	var sb strings.Builder

	// Entry type
	entryType := entryTypeToString(entry.EntryType)
	fmt.Fprintf(&sb, "@%s{%s,\n", entryType, entry.CitationKey)

	// Required/common fields first
	if entry.Title != "" {
		fmt.Fprintf(&sb, "  title = {%s},\n", escapeBibtex(entry.Title))
	}

	// Authors
	if len(entry.Author) > 0 {
		authors := formatPersons(entry.Author)
		fmt.Fprintf(&sb, "  author = {%s},\n", authors)
	}

	// Editors
	if len(entry.Editor) > 0 {
		editors := formatPersons(entry.Editor)
		fmt.Fprintf(&sb, "  editor = {%s},\n", editors)
	}

	// Year and month
	if entry.Year != "" {
		fmt.Fprintf(&sb, "  year = {%s},\n", entry.Year)
	}
	if entry.Month != "" {
		fmt.Fprintf(&sb, "  month = %s,\n", entry.Month)
	}

	// Journal/booktitle based on type
	if entry.Journal != "" {
		fmt.Fprintf(&sb, "  journal = {%s},\n", escapeBibtex(entry.Journal))
	}
	if entry.Booktitle != "" {
		fmt.Fprintf(&sb, "  booktitle = {%s},\n", escapeBibtex(entry.Booktitle))
	}

	// Publisher info
	if entry.Publisher != "" {
		fmt.Fprintf(&sb, "  publisher = {%s},\n", escapeBibtex(entry.Publisher))
	}
	if entry.Address != "" {
		fmt.Fprintf(&sb, "  address = {%s},\n", escapeBibtex(entry.Address))
	}

	// Volume/number/pages
	if entry.Volume != "" {
		fmt.Fprintf(&sb, "  volume = {%s},\n", entry.Volume)
	}
	if entry.Number != "" {
		fmt.Fprintf(&sb, "  number = {%s},\n", entry.Number)
	}
	if entry.Pages != "" {
		fmt.Fprintf(&sb, "  pages = {%s},\n", entry.Pages)
	}

	// Series and edition
	if entry.Series != "" {
		fmt.Fprintf(&sb, "  series = {%s},\n", escapeBibtex(entry.Series))
	}
	if entry.Edition != "" {
		fmt.Fprintf(&sb, "  edition = {%s},\n", escapeBibtex(entry.Edition))
	}

	// Thesis-specific
	if entry.School != "" {
		fmt.Fprintf(&sb, "  school = {%s},\n", escapeBibtex(entry.School))
	}
	if entry.Institution != "" {
		fmt.Fprintf(&sb, "  institution = {%s},\n", escapeBibtex(entry.Institution))
	}
	if entry.Type != "" {
		fmt.Fprintf(&sb, "  type = {%s},\n", escapeBibtex(entry.Type))
	}

	// Identifiers
	if entry.Doi != "" {
		fmt.Fprintf(&sb, "  doi = {%s},\n", entry.Doi)
	}
	if entry.Isbn != "" {
		fmt.Fprintf(&sb, "  isbn = {%s},\n", entry.Isbn)
	}
	if entry.Issn != "" {
		fmt.Fprintf(&sb, "  issn = {%s},\n", entry.Issn)
	}
	if entry.Url != "" {
		fmt.Fprintf(&sb, "  url = {%s},\n", entry.Url)
	}

	// Eprint (arXiv)
	if entry.Eprint != "" {
		fmt.Fprintf(&sb, "  eprint = {%s},\n", entry.Eprint)
		if entry.Eprinttype != "" {
			fmt.Fprintf(&sb, "  eprinttype = {%s},\n", entry.Eprinttype)
		}
		if entry.Primaryclass != "" {
			fmt.Fprintf(&sb, "  primaryclass = {%s},\n", entry.Primaryclass)
		}
	}

	// Keywords
	if len(entry.Keywords) > 0 {
		fmt.Fprintf(&sb, "  keywords = {%s},\n", strings.Join(entry.Keywords, ", "))
	}

	// Abstract
	if entry.Abstract != "" {
		fmt.Fprintf(&sb, "  abstract = {%s},\n", escapeBibtex(entry.Abstract))
	}

	// Note
	if entry.Note != "" {
		fmt.Fprintf(&sb, "  note = {%s},\n", escapeBibtex(entry.Note))
	}

	// Language
	if entry.Language != "" {
		fmt.Fprintf(&sb, "  language = {%s},\n", entry.Language)
	}

	sb.WriteString("}\n")
	return sb.String()
}

// entryTypeToString converts entry type enum to string.
func entryTypeToString(et bibtexv1.EntryType) string {
	switch et {
	case bibtexv1.EntryType_ENTRY_TYPE_ARTICLE:
		return "article"
	case bibtexv1.EntryType_ENTRY_TYPE_BOOK:
		return "book"
	case bibtexv1.EntryType_ENTRY_TYPE_BOOKLET:
		return "booklet"
	case bibtexv1.EntryType_ENTRY_TYPE_INBOOK:
		return "inbook"
	case bibtexv1.EntryType_ENTRY_TYPE_INCOLLECTION:
		return "incollection"
	case bibtexv1.EntryType_ENTRY_TYPE_INPROCEEDINGS:
		return "inproceedings"
	case bibtexv1.EntryType_ENTRY_TYPE_CONFERENCE:
		return "conference"
	case bibtexv1.EntryType_ENTRY_TYPE_MANUAL:
		return "manual"
	case bibtexv1.EntryType_ENTRY_TYPE_MASTERSTHESIS:
		return "mastersthesis"
	case bibtexv1.EntryType_ENTRY_TYPE_PHDTHESIS:
		return "phdthesis"
	case bibtexv1.EntryType_ENTRY_TYPE_PROCEEDINGS:
		return "proceedings"
	case bibtexv1.EntryType_ENTRY_TYPE_TECHREPORT:
		return "techreport"
	case bibtexv1.EntryType_ENTRY_TYPE_UNPUBLISHED:
		return "unpublished"
	case bibtexv1.EntryType_ENTRY_TYPE_ONLINE:
		return "online"
	case bibtexv1.EntryType_ENTRY_TYPE_DATASET:
		return "dataset"
	case bibtexv1.EntryType_ENTRY_TYPE_SOFTWARE:
		return "software"
	case bibtexv1.EntryType_ENTRY_TYPE_PATENT:
		return "patent"
	case bibtexv1.EntryType_ENTRY_TYPE_REPORT:
		return "report"
	case bibtexv1.EntryType_ENTRY_TYPE_THESIS:
		return "thesis"
	default:
		return "misc"
	}
}

// formatPersons formats a list of persons for BibTeX.
func formatPersons(persons []*bibtexv1.Person) string {
	var names []string
	for _, p := range persons {
		name := formatPerson(p)
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, " and ")
}

// formatPerson formats a single person for BibTeX.
func formatPerson(p *bibtexv1.Person) string {
	if p.Name != "" {
		return escapeBibtex(p.Name)
	}
	if p.Family != "" {
		if p.Given != "" {
			name := p.Family + ", " + p.Given
			if p.Suffix != "" {
				name += ", " + p.Suffix
			}
			return escapeBibtex(name)
		}
		return escapeBibtex(p.Family)
	}
	return ""
}

// escapeBibtex escapes special characters for BibTeX.
func escapeBibtex(s string) string {
	// BibTeX special characters that need escaping
	s = strings.ReplaceAll(s, "&", "\\&")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "$", "\\$")
	s = strings.ReplaceAll(s, "#", "\\#")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
