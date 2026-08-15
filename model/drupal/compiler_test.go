package drupal

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

func TestCompileBuildsFullOrderedModel(t *testing.T) {
	configs := []ConfigFile{
		{Name: "field.field.node.page.field_identifier.yml", Data: []byte(`
id: node.page.field_identifier
field_name: field_identifier
entity_type: node
bundle: page
label: Identifier
required: true
field_type: string
settings: { display_summary: false }
`)},
		{Name: "rdf.mapping.node.article.yml", Data: []byte(`
id: node.article
targetEntityType: node
bundle: article
types: ['schema:ScholarlyArticle', 'pcdm:Object']
fieldMappings:
  title:
    properties: ['dcterms:title']
  field_identifier:
    properties: ['dcterms:identifier']
`)},
		{Name: "field.storage.node.field_identifier.yml", Data: []byte(`
id: node.field_identifier
field_name: field_identifier
entity_type: node
type: string
cardinality: -1
settings: { max_length: 255, case_sensitive: false }
`)},
		{Name: "system.site.yml", Data: []byte("uuid: site-uuid\nname: Example Repository\n")},
		{Name: "field.field.node.article.field_identifier.yml", Data: []byte(`
id: node.article.field_identifier
field_name: field_identifier
entity_type: node
bundle: article
label: Work Identifier
required: false
field_type: string
settings: { display_summary: true }
`)},
		{Name: "rdf.mapping.node.page.yml", Data: []byte(`
id: node.page
targetEntityType: node
bundle: page
types: ['pcdm:Object']
fieldMappings:
  field_identifier:
    properties: ['dcterms:identifier']
`)},
	}

	snapshot, err := Compile(configs, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if snapshot.System != "drupal" || snapshot.Provenance.SiteUUID != "site-uuid" || snapshot.Provenance.ConfigHash == "" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if len(snapshot.Entities) != 2 || snapshot.Entities[0].Bundle != "article" || snapshot.Entities[1].Bundle != "page" {
		t.Fatalf("entity order = %#v", snapshot.Entities)
	}
	article := snapshot.Entities[0]
	if len(article.Fields) != 2 || article.Fields[0].Path != "field_identifier" || article.Fields[1].Path != "title" {
		t.Fatalf("article field order = %#v", article.Fields)
	}
	identifier := article.Fields[0]
	if identifier.Cardinality != -1 || identifier.SourceType != "string" || identifier.Kind != model.ValueText {
		t.Fatalf("identifier shape = %#v", identifier)
	}
	if identifier.StorageSettings["max_length"] != 255 || identifier.InstanceSettings["display_summary"] != true {
		t.Fatalf("identifier settings = storage %#v instance %#v", identifier.StorageSettings, identifier.InstanceSettings)
	}
	if got := article.Fields[1]; got.SourceType != "string" || got.SemanticProperties[0] != "dcterms:title" {
		t.Fatalf("RDF-derived core title = %#v", got)
	}
	page, _ := snapshot.Entity("node", "page")
	if !page.Fields[0].Required {
		t.Fatalf("page identifier did not preserve instance required setting")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("compiled snapshot validation error = %v", err)
	}

	reversed := append([]ConfigFile(nil), configs...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	again, err := Compile(reversed, CompileOptions{})
	if err != nil {
		t.Fatalf("reversed Compile() error = %v", err)
	}
	if snapshot.Fingerprint.Value != again.Fingerprint.Value || snapshot.Provenance.ConfigHash != again.Provenance.ConfigHash {
		t.Fatalf("compile digest depends on input ordering")
	}
}

func TestCompilePreservesReferenceTargetsAndUnknownTypes(t *testing.T) {
	configs := []ConfigFile{
		{Name: "field.storage.node.field_parent.yml", Data: []byte(`
id: node.field_parent
field_name: field_parent
entity_type: node
type: entity_reference
cardinality: 1
settings: { target_type: node }
`)},
		{Name: "field.field.node.article.field_parent.yml", Data: []byte(`
id: node.article.field_parent
field_name: field_parent
entity_type: node
bundle: article
field_type: entity_reference
settings:
  handler_settings:
    target_bundles: { page: page, article: article }
`)},
		{Name: "field.storage.node.field_custom.yml", Data: []byte(`
id: node.field_custom
field_name: field_custom
entity_type: node
type: institution_widget
cardinality: 2
settings: { mode: exact }
`)},
		{Name: "field.field.node.article.field_custom.yml", Data: []byte(`
id: node.article.field_custom
field_name: field_custom
entity_type: node
bundle: article
field_type: institution_widget
settings: { local: true }
`)},
	}
	snapshot, err := Compile(configs, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	custom, _ := snapshot.Field("node", "article", "field_custom")
	if custom.Kind != model.ValueOpaque || custom.Cardinality != 2 || custom.SourceType != "institution_widget" {
		t.Fatalf("custom field = %#v", custom)
	}
	parent, _ := snapshot.Field("node", "article", "field_parent")
	if parent.Reference == nil || parent.Reference.EntityType != "node" || strings.Join(parent.Reference.Bundles, ",") != "article,page" {
		t.Fatalf("reference = %#v", parent.Reference)
	}
}

func TestCompileRejectsReferenceFieldsWithoutTargetType(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name             string
		fieldType        string
		targetType       string
		instanceSettings string
		want             string
	}{
		{name: "entity reference missing target", fieldType: "entity_reference", want: "requires a valid target_type"},
		{name: "typed relation missing target", fieldType: "typed_relation", want: "requires a valid target_type"},
		{name: "invalid target machine name", fieldType: "entity_reference", targetType: "Taxonomy Term", want: "requires a valid target_type"},
		{
			name: "invalid target bundle", fieldType: "entity_reference", targetType: "node",
			instanceSettings: "settings:\n  handler_settings:\n    target_bundles:\n      'Bad Bundle': article\n",
			want:             "has invalid target bundle",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			settings := "settings: {}\n"
			if test.targetType != "" {
				settings = "settings:\n  target_type: '" + test.targetType + "'\n"
			}
			instanceSettings := test.instanceSettings
			if instanceSettings == "" {
				instanceSettings = "settings: {}\n"
			}
			configs := []ConfigFile{
				{
					Name: "field.storage.node.field_broken.yml",
					Data: []byte("id: node.field_broken\nfield_name: field_broken\nentity_type: node\ntype: " + test.fieldType + "\ncardinality: 1\n" + settings),
				},
				{
					Name: "field.field.node.article.field_broken.yml",
					Data: []byte("id: node.article.field_broken\nfield_name: field_broken\nentity_type: node\nbundle: article\nfield_type: " + test.fieldType + "\n" + instanceSettings),
				},
			}
			_, err := Compile(configs, CompileOptions{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCompileIncludesNonNodeEntitiesAndIgnoresUnrelatedConfig(t *testing.T) {
	configs := []ConfigFile{
		{Name: "core.extension.yml", Data: []byte("module: { system: 0 }\n")},
		{Name: "field.storage.taxonomy_term.field_external_id.yml", Data: []byte(`
id: taxonomy_term.field_external_id
field_name: field_external_id
entity_type: taxonomy_term
type: string
cardinality: 1
settings: { max_length: 64 }
`)},
		{Name: "field.field.taxonomy_term.people.field_external_id.yml", Data: []byte(`
id: taxonomy_term.people.field_external_id
field_name: field_external_id
entity_type: taxonomy_term
bundle: people
label: External ID
field_type: string
settings: { case_sensitive: true }
`)},
	}
	snapshot, err := Compile(configs, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	field, exists := snapshot.Field("taxonomy_term", "people", "field_external_id")
	if !exists {
		t.Fatalf("non-node entity was omitted: %#v", snapshot.Entities)
	}
	if field.Cardinality != 1 || field.StorageSettings["max_length"] != 64 || field.InstanceSettings["case_sensitive"] != true {
		t.Fatalf("non-node field settings = %#v", field)
	}
}

func TestCompileBoundsAndArchiveSafety(t *testing.T) {
	config := ConfigFile{Name: "rdf.mapping.node.article.yml", Data: []byte("id: node.article\ntargetEntityType: node\nbundle: article\nfieldMappings: {}\n")}
	_, err := Compile([]ConfigFile{config}, CompileOptions{MaxFileBytes: 8, MaxBytes: 32, MaxFiles: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds 8 bytes") {
		t.Fatalf("bounded Compile() error = %v", err)
	}

	archive := makeArchive(t, "../rdf.mapping.node.article.yml", config.Data)
	_, err = CompileArchive(bytes.NewReader(archive), CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe Drupal config archive path") {
		t.Fatalf("unsafe archive error = %v", err)
	}

	archive = makeArchive(t, "./rdf.mapping.node.article.yml", config.Data)
	if _, err := CompileArchive(bytes.NewReader(archive), CompileOptions{}); err != nil {
		t.Fatalf("archive with conventional ./ prefix error = %v", err)
	}
	if _, err := Compile([]ConfigFile{{Name: "..", Data: nil}, config}, CompileOptions{}); err == nil || !strings.Contains(err.Error(), "unsafe Drupal config name") {
		t.Fatalf("unsafe in-memory config name error = %v", err)
	}
}

func makeArchive(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
