package protoxml_test

import (
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	arxivv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/arxiv/v1_0"
	dcv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/dublincore/v20200120"
	pqv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
	"google.golang.org/protobuf/proto"
)

func TestUnmarshalArXiv(t *testing.T) {
	input := []byte(`<arXivRecord xmlns="http://arXiv.org/arXivRecord" version="1.0">
  <identifier>2511.11447</identifier>
  <primary>cs.CL</primary>
  <cross>cs.AI</cross>
  <cross>cs.LG</cross>
  <submitter>
    <email>test@example.com</email>
    <identifier>user123</identifier>
  </submitter>
  <version>2</version>
  <date>2025-05-19T18:00:00Z</date>
  <source>
    <type>tex</type>
    <size>123456</size>
    <md5>abcdef01234567890abcdef012345678</md5>
  </source>
  <title>A Sample arXiv Paper Title</title>
  <authorship>
    <affiliation affid="1">
      <institution>MIT</institution>
      <address>Cambridge, MA</address>
    </affiliation>
    <author affref="1">
      <beforekey>John</beforekey>
      <keyname>Doe</keyname>
    </author>
    <author>
      <beforekey>Jane</beforekey>
      <keyname>Smith</keyname>
      <afterkey>Jr</afterkey>
    </author>
  </authorship>
  <classification scheme="MSC2000">
    <value>68T50</value>
    <value>68T05</value>
  </classification>
  <alternate>
    <DOI>10.1234/test.2025</DOI>
    <report-no>MIT-TR-2025-01</report-no>
    <journal-ref>Nature 580, 123-128 (2025)</journal-ref>
  </alternate>
  <comments>10 pages, 3 figures</comments>
  <abstract>This is the abstract of the paper.</abstract>
</arXivRecord>`)

	record := &arxivv1.Record{}
	err := protoxml.Unmarshal(input, record)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Identifier
	if record.Identifier != "2511.11447" {
		t.Errorf("Identifier: got %q, want %q", record.Identifier, "2511.11447")
	}

	// Primary
	if record.Primary != "cs.CL" {
		t.Errorf("Primary: got %q, want %q", record.Primary, "cs.CL")
	}

	// Cross-listed
	if len(record.Cross) != 2 {
		t.Fatalf("Cross: got %d, want 2", len(record.Cross))
	}
	if record.Cross[0] != "cs.AI" {
		t.Errorf("Cross[0]: got %q, want %q", record.Cross[0], "cs.AI")
	}
	if record.Cross[1] != "cs.LG" {
		t.Errorf("Cross[1]: got %q, want %q", record.Cross[1], "cs.LG")
	}

	// Submitter
	if record.Submitter == nil {
		t.Fatal("Submitter is nil")
	}
	if record.Submitter.Email != "test@example.com" {
		t.Errorf("Submitter.Email: got %q", record.Submitter.Email)
	}
	if record.Submitter.Identifier != "user123" {
		t.Errorf("Submitter.Identifier: got %q", record.Submitter.Identifier)
	}

	// Version
	if record.Version != 2 {
		t.Errorf("Version: got %d, want 2", record.Version)
	}

	// Date
	if record.Date != "2025-05-19T18:00:00Z" {
		t.Errorf("Date: got %q", record.Date)
	}

	// Source
	if record.Source == nil {
		t.Fatal("Source is nil")
	}
	if record.Source.Size != 123456 {
		t.Errorf("Source.Size: got %d", record.Source.Size)
	}
	if record.Source.Md5 != "abcdef01234567890abcdef012345678" {
		t.Errorf("Source.Md5: got %q", record.Source.Md5)
	}

	// Title
	if record.Title != "A Sample arXiv Paper Title" {
		t.Errorf("Title: got %q", record.Title)
	}

	// Authorship
	if record.Authorship == nil {
		t.Fatal("Authorship is nil")
	}

	// Affiliations
	if len(record.Authorship.Affiliations) != 1 {
		t.Fatalf("Affiliations: got %d, want 1", len(record.Authorship.Affiliations))
	}
	if record.Authorship.Affiliations[0].Institution != "MIT" {
		t.Errorf("Affiliation institution: got %q", record.Authorship.Affiliations[0].Institution)
	}
	if record.Authorship.Affiliations[0].Affid != 1 {
		t.Errorf("Affiliation affid: got %d", record.Authorship.Affiliations[0].Affid)
	}

	// Authors
	if len(record.Authorship.Authors) != 2 {
		t.Fatalf("Authors: got %d, want 2", len(record.Authorship.Authors))
	}
	if record.Authorship.Authors[0].Beforekey != "John" {
		t.Errorf("Author[0].Beforekey: got %q", record.Authorship.Authors[0].Beforekey)
	}
	if record.Authorship.Authors[0].Keyname != "Doe" {
		t.Errorf("Author[0].Keyname: got %q", record.Authorship.Authors[0].Keyname)
	}
	if record.Authorship.Authors[0].Affref != "1" {
		t.Errorf("Author[0].Affref: got %q", record.Authorship.Authors[0].Affref)
	}

	if record.Authorship.Authors[1].Afterkey != "Jr" {
		t.Errorf("Author[1].Afterkey: got %q", record.Authorship.Authors[1].Afterkey)
	}

	// Classification
	if len(record.Classification) != 1 {
		t.Fatalf("Classification: got %d, want 1", len(record.Classification))
	}
	if record.Classification[0].Scheme != arxivv1.ClassificationScheme_CLASSIFICATION_SCHEME_MSC2000 {
		t.Errorf("Classification scheme: got %v", record.Classification[0].Scheme)
	}
	if len(record.Classification[0].Value) != 2 {
		t.Fatalf("Classification values: got %d, want 2", len(record.Classification[0].Value))
	}

	// Alternate
	if record.Alternate == nil {
		t.Fatal("Alternate is nil")
	}
	if len(record.Alternate.Doi) != 1 || record.Alternate.Doi[0] != "10.1234/test.2025" {
		t.Errorf("Alternate.Doi: got %v", record.Alternate.Doi)
	}
	if len(record.Alternate.ReportNo) != 1 || record.Alternate.ReportNo[0] != "MIT-TR-2025-01" {
		t.Errorf("Alternate.ReportNo: got %v", record.Alternate.ReportNo)
	}
	if len(record.Alternate.JournalRef) != 1 || record.Alternate.JournalRef[0] != "Nature 580, 123-128 (2025)" {
		t.Errorf("Alternate.JournalRef: got %v", record.Alternate.JournalRef)
	}

	// Comments
	if len(record.Comments) != 1 || record.Comments[0] != "10 pages, 3 figures" {
		t.Errorf("Comments: got %v", record.Comments)
	}

	// Abstract
	if len(record.Abstract) != 1 || record.Abstract[0] != "This is the abstract of the paper." {
		t.Errorf("Abstract: got %v", record.Abstract)
	}
}

func TestUnmarshalProQuest(t *testing.T) {
	input := []byte(`<DISS_submission embargo_code="3">
  <DISS_authorship>
    <DISS_author type="primary">
      <DISS_name>
        <DISS_surname>Smith</DISS_surname>
        <DISS_fname>John</DISS_fname>
        <DISS_middle>Q</DISS_middle>
      </DISS_name>
      <DISS_orcid>0000-0001-2345-6789</DISS_orcid>
    </DISS_author>
  </DISS_authorship>
  <DISS_description page_count="150">
    <DISS_title>Test Dissertation Title</DISS_title>
    <DISS_degree>Doctor of Philosophy</DISS_degree>
    <DISS_degree_level>Doctoral</DISS_degree_level>
  </DISS_description>
  <DISS_content>
    <DISS_abstract>
      <DISS_para>First paragraph.</DISS_para>
      <DISS_para>Second paragraph.</DISS_para>
    </DISS_abstract>
  </DISS_content>
</DISS_submission>`)

	submission := &pqv1.Submission{}
	err := protoxml.Unmarshal(input, submission)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Embargo code (attribute)
	if submission.EmbargoCode != 3 {
		t.Errorf("EmbargoCode: got %d, want 3", submission.EmbargoCode)
	}

	// Description
	if submission.Description == nil {
		t.Fatal("Description is nil")
	}
	if submission.Description.Title != "Test Dissertation Title" {
		t.Errorf("Title: got %q", submission.Description.Title)
	}
	if submission.Description.Degree != "Doctor of Philosophy" {
		t.Errorf("Degree: got %q", submission.Description.Degree)
	}
	if submission.Description.PageCount != 150 {
		t.Errorf("PageCount: got %d", submission.Description.PageCount)
	}

	// Authorship
	if submission.Authorship == nil {
		t.Fatal("Authorship is nil")
	}
	if len(submission.Authorship.Authors) != 1 {
		t.Fatalf("Authors: got %d, want 1", len(submission.Authorship.Authors))
	}
	author := submission.Authorship.Authors[0]
	if author.Type != "primary" {
		t.Errorf("Author.Type: got %q", author.Type)
	}
	if author.Name == nil {
		t.Fatal("Author.Name is nil")
	}
	if author.Name.Surname != "Smith" {
		t.Errorf("Author.Name.Surname: got %q", author.Name.Surname)
	}
	if author.Name.First != "John" {
		t.Errorf("Author.Name.First: got %q", author.Name.First)
	}
	if author.Orcid != "0000-0001-2345-6789" {
		t.Errorf("Author.Orcid: got %q", author.Orcid)
	}

	// Abstract paragraphs
	if submission.Content == nil || submission.Content.Abstract == nil {
		t.Fatal("Content.Abstract is nil")
	}
	if len(submission.Content.Abstract.Paragraphs) != 2 {
		t.Fatalf("Abstract paragraphs: got %d, want 2", len(submission.Content.Abstract.Paragraphs))
	}
	if submission.Content.Abstract.Paragraphs[0] != "First paragraph." {
		t.Errorf("Abstract[0]: got %q", submission.Content.Abstract.Paragraphs[0])
	}
}

func TestUnmarshalDublinCoreNamespaces(t *testing.T) {
	input := []byte(`<metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/">
  <dc:title>A Dublin Core Record</dc:title>
  <dc:creator>John Doe</dc:creator>
  <dc:creator>Jane Smith</dc:creator>
  <dc:subject>Computer Science</dc:subject>
  <dc:description>A test description.</dc:description>
  <dc:publisher>Test Publisher</dc:publisher>
  <dc:date>2024-01-15</dc:date>
  <dc:identifier>doi:10.1234/test</dc:identifier>
  <dc:language>en</dc:language>
  <dcterms:issued>2024-06-01</dcterms:issued>
  <dcterms:abstract>A more detailed abstract.</dcterms:abstract>
</metadata>`)

	dcv1 := getDCRecordType()
	if dcv1 == nil {
		t.Skip("Dublin Core proto not available")
	}

	err := protoxml.Unmarshal(input, dcv1)
	if err != nil {
		t.Fatalf("Unmarshal DC failed: %v", err)
	}

	// Verify via proto reflection that title was set
	msgRef := dcv1.ProtoReflect()
	titleFd := msgRef.Descriptor().Fields().ByName("title")
	if titleFd == nil {
		t.Fatal("title field not found in DC proto")
	}
	if !msgRef.Has(titleFd) {
		t.Error("title field not populated")
	} else {
		// Title is repeated LocalizedString message
		list := msgRef.Get(titleFd).List()
		if list.Len() != 1 {
			t.Errorf("title: got %d elements, want 1", list.Len())
		}
	}

	creatorFd := msgRef.Descriptor().Fields().ByName("creator")
	if creatorFd != nil && msgRef.Has(creatorFd) {
		list := msgRef.Get(creatorFd).List()
		if list.Len() != 2 {
			t.Errorf("creator: got %d elements, want 2", list.Len())
		}
	}

	issuedFd := msgRef.Descriptor().Fields().ByName("issued")
	if issuedFd != nil && msgRef.Has(issuedFd) {
		val := msgRef.Get(issuedFd).String()
		if val != "2024-06-01" {
			t.Errorf("issued: got %q, want %q", val, "2024-06-01")
		}
	}
}

// getDCRecordType returns a new DC Record proto message.
func getDCRecordType() proto.Message {
	return &dcv1.Record{}
}

func TestUnmarshalEmpty(t *testing.T) {
	input := []byte(`<arXivRecord/>`)
	record := &arxivv1.Record{}
	err := protoxml.Unmarshal(input, record)
	if err != nil {
		t.Fatalf("Unmarshal empty failed: %v", err)
	}
}

func TestUnmarshalMissing(t *testing.T) {
	input := []byte(`<notAnArxivRecord/>`)
	record := &arxivv1.Record{}
	err := protoxml.Unmarshal(input, record)
	if err == nil {
		t.Error("Expected error for missing element")
	}
}

func TestUnmarshalSkipsUnknown(t *testing.T) {
	input := []byte(`<arXivRecord>
  <identifier>test</identifier>
  <unknownElement>should be skipped</unknownElement>
  <title>Test Title</title>
</arXivRecord>`)

	record := &arxivv1.Record{}
	err := protoxml.Unmarshal(input, record)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if record.Title != "Test Title" {
		t.Errorf("Title: got %q", record.Title)
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	// Test round-trip with arXiv (simpler, all child elements, no nested attrs)
	arxivRec := &arxivv1.Record{
		Identifier: "2511.99999",
		Primary:    "cs.CL",
		Cross:      []string{"cs.AI"},
		Version:    1,
		Date:       "2025-06-15T00:00:00Z",
		Title:      "Round Trip Paper",
		Abstract:   []string{"Abstract text."},
		Comments:   []string{"5 pages"},
	}

	data, err := protoxml.Marshal(arxivRec)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	parsed := &arxivv1.Record{}
	err = protoxml.Unmarshal(data, parsed)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.Identifier != arxivRec.Identifier {
		t.Errorf("Identifier: got %q, want %q", parsed.Identifier, arxivRec.Identifier)
	}
	if parsed.Title != arxivRec.Title {
		t.Errorf("Title: got %q, want %q", parsed.Title, arxivRec.Title)
	}
	if parsed.Version != arxivRec.Version {
		t.Errorf("Version: got %d, want %d", parsed.Version, arxivRec.Version)
	}
	if len(parsed.Cross) != 1 || parsed.Cross[0] != "cs.AI" {
		t.Errorf("Cross: got %v", parsed.Cross)
	}
	if len(parsed.Abstract) != 1 || parsed.Abstract[0] != "Abstract text." {
		t.Errorf("Abstract: got %v", parsed.Abstract)
	}
}
