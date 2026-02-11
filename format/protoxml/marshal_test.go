package protoxml_test

import (
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/format/protoxml"
	pqv1 "github.com/lehigh-university-libraries/crosswalk/gen/go/spoke/proquest/v1"
)

func TestMarshal(t *testing.T) {
	// Create a simple ProQuest submission to test marshaling
	submission := &pqv1.Submission{
		EmbargoCode: 0,
		Description: &pqv1.Description{
			Title:       "Test Dissertation",
			Degree:      "Doctor of Philosophy",
			DegreeLevel: "Doctoral",
			Discipline:  "Computer Science",
			PageCount:   150,
		},
		Authorship: &pqv1.Authorship{
			Authors: []*pqv1.Author{
				{
					Type: "primary",
					Name: &pqv1.Name{
						Surname: "Smith",
						First:   "John",
						Middle:  "Q",
					},
					Orcid: "0000-0001-2345-6789",
				},
			},
		},
		Content: &pqv1.Content{
			Abstract: &pqv1.Abstract{
				Paragraphs: []string{
					"This is the first paragraph of the abstract.",
					"This is the second paragraph.",
				},
			},
		},
	}

	data, err := protoxml.Marshal(submission)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	output := string(data)

	// Verify key elements are present with ProQuest XML names from annotations
	checks := []string{
		"<DISS_submission>",
		"</DISS_submission>",
		"<DISS_description>",
		"<DISS_title>Test Dissertation</DISS_title>",
		"<DISS_degree>Doctor of Philosophy</DISS_degree>",
		"<DISS_authorship>",
		"<DISS_surname>Smith</DISS_surname>",
		"<DISS_fname>John</DISS_fname>",
		"<DISS_abstract>",
	}

	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("Expected output to contain %q, got:\n%s", check, output)
		}
	}
}

func TestMarshalWithOptions(t *testing.T) {
	submission := &pqv1.Submission{
		Description: &pqv1.Description{
			Title: "Test",
		},
	}

	// Test with custom indentation
	opts := protoxml.MarshalOptions{Indent: "\t"}
	data, err := protoxml.MarshalWithOptions(submission, opts)
	if err != nil {
		t.Fatalf("MarshalWithOptions failed: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "\t<DISS_description>") {
		t.Errorf("Expected tab indentation, got:\n%s", output)
	}
}

func TestWriteTo(t *testing.T) {
	submission := &pqv1.Submission{
		Description: &pqv1.Description{
			Title: "Test",
		},
	}

	var buf strings.Builder
	err := protoxml.WriteTo(&buf, submission)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	output := buf.String()

	// Should include XML header
	if !strings.HasPrefix(output, "<?xml version=") {
		t.Errorf("Expected XML header, got:\n%s", output)
	}
}

func TestMarshalEscaping(t *testing.T) {
	submission := &pqv1.Submission{
		Description: &pqv1.Description{
			Title: "Test <with> \"special\" & 'characters'",
		},
	}

	data, err := protoxml.Marshal(submission)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	output := string(data)

	// Verify special characters are escaped
	if strings.Contains(output, "<with>") {
		t.Error("Expected '<' to be escaped")
	}
	if !strings.Contains(output, "&lt;with&gt;") {
		t.Errorf("Expected escaped characters, got:\n%s", output)
	}
}

func TestMarshalEmptyMessage(t *testing.T) {
	submission := &pqv1.Submission{}

	data, err := protoxml.Marshal(submission)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	output := string(data)

	// Empty message should produce self-closing tag with xml_name from annotation
	if !strings.Contains(output, "<DISS_submission/>") {
		t.Errorf("Expected self-closing tag for empty message, got:\n%s", output)
	}
}
