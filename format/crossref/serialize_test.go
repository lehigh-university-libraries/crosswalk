package crossref

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format"
	hubv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/hub/v1"
)

func TestSerializeJournalVolumeAsJournalArticle(t *testing.T) {
	record := &hubv1.Record{
		Title: "Impact of the Allentown NIZ on the Location of Business Activity",
		Contributors: []*hubv1.Contributor{{
			ParsedName: &hubv1.ParsedName{
				Given:  "Thomas",
				Family: "Hyclak",
			},
			Role: "author",
		}},
		Dates: []*hubv1.DateValue{{
			Type:  hubv1.DateType_DATE_TYPE_ISSUED,
			Year:  2025,
			Month: 7,
		}},
		Genres: []*hubv1.Subject{{
			Value: "articles",
			Uri:   "http://vocab.getty.edu/page/aat/300048715",
		}},
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.18275/martindale-pb-v006"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_URL, Value: "https://preserve.lehigh.edu/node/453242"},
		},
		Relations: []*hubv1.Relation{{
			Type:        hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
			TargetTitle: "Martindale Policy Briefs",
			TargetUri:   "https://preserve.lehigh.edu/node/453223",
			Description: `{"source":"drupal","title":"Martindale Policy Briefs","doi":"10.18275/martindale-pb","resource":"https://preserve.lehigh.edu/node/453223","genres":["http://vocab.getty.edu/page/aat/300215390"]}`,
		}},
	}

	var out bytes.Buffer
	if err := (&Format{}).Serialize(&out, []*hubv1.Record{record}, &format.SerializeOptions{
		Pretty: true,
		ReferenceDOIs: []string{
			"doi.org/10.4018/IJEBR.2019010101",
			"https://doi.org/10.1108/JMLC-03-2018- 0023",
			"doi.org/10.4018/IJEBR.2019010101",
		},
	}); err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	xml := out.String()
	for _, want := range []string{
		`<journal>`,
		`<journal_metadata language="en">`,
		`<full_title>Martindale Policy Briefs</full_title>`,
		`<doi>10.18275/martindale-pb</doi>`,
		`<resource>https://preserve.lehigh.edu/node/453223</resource>`,
		`<journal_article publication_type="full_text">`,
		`<title>Impact of the Allentown NIZ on the Location of Business Activity</title>`,
		`<surname>Hyclak</surname>`,
		`<publication_date media_type="online">`,
		`<month>7</month>`,
		`<year>2025</year>`,
		`<doi>10.18275/martindale-pb-v006</doi>`,
		`<resource>https://preserve.lehigh.edu/node/453242</resource>`,
		`<citation_list>`,
		`<citation key="ref1">`,
		`<doi>10.4018/IJEBR.2019010101</doi>`,
		`<citation key="ref2">`,
		`<doi>10.1108/JMLC-03-2018-0023</doi>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("expected XML to contain %q:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<posted_content") {
		t.Fatalf("volume record serialized as posted_content:\n%s", xml)
	}
	if strings.Contains(xml, "<database") {
		t.Fatalf("unexpected database element:\n%s", xml)
	}
	if strings.Index(xml, "<month>7</month>") > strings.Index(xml, "<year>2025</year>") {
		t.Fatalf("publication_date elements are not in Crossref order:\n%s", xml)
	}
}

func TestSerializeJournalArticleWithIssueParentUsesGrandparentJournal(t *testing.T) {
	record := &hubv1.Record{
		Title: "Article in an issue",
		Genres: []*hubv1.Subject{{
			Value: "articles",
			Uri:   "http://vocab.getty.edu/page/aat/300048715",
		}},
		Dates: []*hubv1.DateValue{{
			Type: hubv1.DateType_DATE_TYPE_ISSUED,
			Year: 2026,
		}},
		Identifiers: []*hubv1.Identifier{
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_DOI, Value: "10.18275/example-article"},
			{Type: hubv1.IdentifierType_IDENTIFIER_TYPE_URL, Value: "https://preserve.lehigh.edu/node/3"},
		},
		Relations: []*hubv1.Relation{{
			Type:        hubv1.RelationType_RELATION_TYPE_MEMBER_OF,
			TargetTitle: "Issue 1",
			TargetUri:   "https://preserve.lehigh.edu/node/2",
			Description: `{"source":"drupal","title":"Issue 1","doi":"10.18275/example-issue","resource":"https://preserve.lehigh.edu/node/2","genres":["http://vocab.getty.edu/page/aat/300312349"],"parent":{"source":"drupal","title":"Example Journal","doi":"10.18275/example-journal","resource":"https://preserve.lehigh.edu/node/1","genres":["http://vocab.getty.edu/page/aat/300215390"]}}`,
		}},
	}

	var out bytes.Buffer
	if err := (&Format{}).Serialize(&out, []*hubv1.Record{record}, &format.SerializeOptions{Pretty: true}); err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	xml := out.String()
	for _, want := range []string{
		`<full_title>Example Journal</full_title>`,
		`<doi>10.18275/example-journal</doi>`,
		`<journal_issue>`,
		`<year>2026</year>`,
		`<doi>10.18275/example-issue</doi>`,
		`<resource>https://preserve.lehigh.edu/node/2</resource>`,
		`<journal_article publication_type="full_text">`,
		`<title>Article in an issue</title>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("expected XML to contain %q:\n%s", want, xml)
		}
	}
}
