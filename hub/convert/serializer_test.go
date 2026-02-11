package convert

import (
	"testing"
)

func TestSerializerRegistry_Register(t *testing.T) {
	r := NewSerializerRegistry()

	r.Register("test_serializer", func(value any, opts *SerializerOptions) (string, error) {
		return "serialized:" + value.(string), nil
	})

	fn, ok := r.Get("test_serializer")
	if !ok {
		t.Fatal("custom serializer not found")
	}

	result, err := fn("value", nil)
	if err != nil {
		t.Fatalf("serializer error: %v", err)
	}

	if result != "serialized:value" {
		t.Errorf("result = %q, want 'serialized:value'", result)
	}
}

func TestSerializerRegistry_DefaultSerializers(t *testing.T) {
	r := DefaultSerializers()

	expectedSerializers := []string{
		"passthrough", "year", "iso8601", "edtf", "join",
		"bibtex_name", "csl_name", "doi_url", "orcid_url",
	}

	for _, name := range expectedSerializers {
		if _, ok := r.Get(name); !ok {
			t.Errorf("default serializer %q not found", name)
		}
	}
}

func TestSerializePassthrough(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "hello", "hello"},
		{"integer", 42, "42"},
		{"nil", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializePassthrough(tt.value, nil)
			if err != nil {
				t.Fatalf("serializePassthrough error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializePassthrough(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeYear(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"year string", "2023", "2023"},
		{"full date", "2023-05-15", "2023"},
		{"with text", "Published in 2023", "2023"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeYear(tt.value, nil)
			if err != nil {
				t.Fatalf("serializeYear error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeYear(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeISO8601(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"full date", "2023-05-15", "2023-05-15"},
		{"year-month", "2023-05", "2023-05-01"},
		{"year only", "2023", "2023-01-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeISO8601(tt.value, nil)
			if err != nil {
				t.Fatalf("serializeISO8601 error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeISO8601(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeJoin(t *testing.T) {
	tests := []struct {
		name      string
		value     any
		delimiter string
		want      string
	}{
		{"string slice", []string{"a", "b", "c"}, ", ", "a, b, c"},
		{"custom delimiter", []string{"a", "b", "c"}, ";", "a;b;c"},
		{"single string", "hello", "", "hello"},
		{"any slice", []any{"a", 1, "c"}, ", ", "a, 1, c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &SerializerOptions{JoinDelimiter: tt.delimiter}
			result, err := serializeJoin(tt.value, opts)
			if err != nil {
				t.Fatalf("serializeJoin error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeJoin(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeBibTeXName(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			"bibtex name struct",
			BibTeXName{Given: "John", Family: "Smith"},
			"Smith, John",
		},
		{
			"with suffix",
			BibTeXName{Given: "John", Family: "Smith", Suffix: "Jr."},
			"Smith, John, Jr.",
		},
		{
			"family only",
			BibTeXName{Family: "Smith"},
			"Smith",
		},
		{
			"csl name struct",
			CSLName{Given: "Jane", Family: "Doe"},
			"Doe, Jane",
		},
		{
			"csl literal",
			CSLName{Literal: "World Health Organization"},
			"World Health Organization",
		},
		{
			"map",
			map[string]any{"family": "Brown", "given": "Charlie"},
			"Brown, Charlie",
		},
		{
			"string",
			"Already Formatted",
			"Already Formatted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeBibTeXName(tt.value, nil)
			if err != nil {
				t.Fatalf("serializeBibTeXName error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeBibTeXName(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeDOIURL(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"raw doi", "10.1234/foo.bar", "https://doi.org/10.1234/foo.bar"},
		{"with prefix", "doi:10.1234/foo.bar", "https://doi.org/10.1234/foo.bar"},
		{"already url", "https://doi.org/10.1234/foo", "https://doi.org/10.1234/foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeDOIURL(tt.value, nil)
			if err != nil {
				t.Fatalf("serializeDOIURL error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeDOIURL(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}

func TestSerializeORCIDURL(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"formatted orcid", "0000-0002-1825-0097", "https://orcid.org/0000-0002-1825-0097"},
		{"already url", "https://orcid.org/0000-0002-1825-0097", "https://orcid.org/0000-0002-1825-0097"},
		{"with prefix", "orcid:0000-0002-1825-0097", "https://orcid.org/0000-0002-1825-0097"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeORCIDURL(tt.value, nil)
			if err != nil {
				t.Fatalf("serializeORCIDURL error: %v", err)
			}
			if result != tt.want {
				t.Errorf("serializeORCIDURL(%v) = %q, want %q", tt.value, result, tt.want)
			}
		})
	}
}
