package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"github.com/lehigh-university-libraries/crosswalk/profile"
	"gopkg.in/yaml.v3"
)

func TestProfileCreateDrupalDraftValidatePublishWorkflow(t *testing.T) {
	useProfileCommandConfig(t)
	draftPath := filepath.Join(t.TempDir(), "draft.yaml")
	command := newProfileCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{
		"create", "drupal", "example",
		"--config", filepath.Join("..", "spec", "testdata", "drupal"),
		"--entity-type", "node", "--bundle", "islandora_object",
		"--output", draftPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("profile create Execute() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("profile create stdout = %q, want empty when --output is used", output.String())
	}
	if exists, err := profile.DefinitionExists("example"); err != nil || exists {
		t.Fatalf("draft was prematurely published: exists=%v err=%v", exists, err)
	}
	draftData, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := profile.DecodeDefinition(bytes.NewReader(draftData))
	if err != nil {
		t.Fatalf("DecodeDefinition(draft) error = %v", err)
	}
	if _, err := profile.LoadModel(draft.ModelFingerprint); err != nil {
		t.Fatalf("draft model was not retained: %v", err)
	}
	if draft.System != "drupal" || draft.Identity == nil || draft.Identity.Repository.Bundle != "islandora_object" {
		t.Fatalf("draft = %#v", draft)
	}
	for _, rule := range draft.Identity.Identifiers {
		if rule.Scheme == "local" || rule.Scheme == "pid" {
			t.Fatalf("unscoped local identifier was generated: %#v", rule)
		}
	}

	// Editing executable policy invalidates the draft's old fingerprint. The
	// validate phase deliberately accepts that authoring state, reseals it, and
	// checks every mapping against the retained model.
	draft.Description = "Reviewed profile"
	draft.Mappings[0].Merge = profile.MergeReplace
	edited, err := yaml.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	sealedPath := filepath.Join(t.TempDir(), "sealed.yaml")
	validate := newProfileValidateCmd()
	validate.SetArgs([]string{"--input", draftPath, "--output", sealedPath})
	if err := validate.Execute(); err != nil {
		t.Fatalf("profile validate Execute() error = %v", err)
	}
	sealedData, err := os.ReadFile(sealedPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := profile.DecodeDefinition(bytes.NewReader(sealedData))
	if err != nil {
		t.Fatalf("sealed definition is invalid: %v", err)
	}
	if sealed.Fingerprint.Value == draft.Fingerprint.Value {
		t.Fatal("edited executable mapping retained the stale draft fingerprint")
	}

	publish := newProfilePublishCmd()
	publish.SetOut(&output)
	publish.SetArgs([]string{"--input", sealedPath})
	if err := publish.Execute(); err != nil {
		t.Fatalf("profile publish Execute() error = %v", err)
	}
	stored, err := profile.LoadStored("example")
	if err != nil || stored.Definition.Fingerprint.Value != sealed.Fingerprint.Value {
		t.Fatalf("stored pair = %#v, %v", stored, err)
	}
}

func TestProfileCreateDrupalAcceptsArchiveAndExplicitInstitutionPolicy(t *testing.T) {
	useProfileCommandConfig(t)
	archivePath := archiveDirectory(t, filepath.Join("..", "spec", "testdata", "drupal"))
	command := newProfileCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{
		"create", "drupal", "archive",
		"--config", archivePath, "--bundle", "islandora_object",
		"--institution-attribute", "accession",
		"--institution-scheme", "example-accession",
		"--institution-namespace", "https://repository.example.edu/id/accession/",
		"--institution-pattern", `^[A-Z]{3}-[0-9]{6}$`,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("archive profile create Execute() error = %v", err)
	}
	draft, err := profile.DecodeDefinition(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rules := draft.Identity.Identifiers
	if len(rules) == 0 || rules[len(rules)-1].Scheme != "example-accession" || rules[len(rules)-1].Namespace != "https://repository.example.edu/id/accession/" {
		t.Fatalf("institution identifier rules = %#v", rules)
	}
}

func TestProfileCreateOmekaSDraftValidatePublishWorkflow(t *testing.T) {
	useProfileCommandConfig(t)
	draftPath := filepath.Join(t.TempDir(), "omeka-draft.yaml")
	command := newProfileCmd()
	command.SetErr(io.Discard)
	command.SetArgs([]string{
		"create", "omeka-s", "photographs",
		"--snapshot", filepath.Join("..", "format", "omeka_s", "testdata", "custom_snapshot.json"),
		"--resource-template-id", "200",
		"--institution-field", "local:department",
		"--institution-scheme", "example-accession",
		"--institution-namespace", "https://collections.example.edu/id/",
		"--institution-pattern", `^EX-[0-9]{6}$`,
		"--output", draftPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("profile create omeka-s Execute() error = %v", err)
	}
	data, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := profile.DecodeDefinition(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if draft.System != "omeka-s" || draft.Identity == nil || draft.Identity.Repository.Bundle != "200" {
		t.Fatalf("Omeka draft = %#v", draft)
	}
	if _, err := profile.LoadModel(draft.ModelFingerprint); err != nil {
		t.Fatalf("Omeka draft model was not retained: %v", err)
	}
	if exists, err := profile.DefinitionExists("photographs"); err != nil || exists {
		t.Fatalf("Omeka draft was prematurely published: exists=%v err=%v", exists, err)
	}

	sealedPath := filepath.Join(t.TempDir(), "omeka-sealed.yaml")
	validate := newProfileValidateCmd()
	validate.SetArgs([]string{"--input", draftPath, "--output", sealedPath})
	if err := validate.Execute(); err != nil {
		t.Fatalf("profile validate Omeka draft error = %v", err)
	}
	publish := newProfilePublishCmd()
	publish.SetOut(io.Discard)
	publish.SetArgs([]string{"--input", sealedPath})
	if err := publish.Execute(); err != nil {
		t.Fatalf("profile publish Omeka definition error = %v", err)
	}
	stored, err := profile.LoadStored("photographs")
	if err != nil || stored.Definition.System != "omeka-s" {
		t.Fatalf("stored Omeka pair = %#v, %v", stored, err)
	}
}

func TestProfileCreateOmekaSRejectsSymlinkSnapshot(t *testing.T) {
	useProfileCommandConfig(t)
	target := filepath.Join("..", "format", "omeka_s", "testdata", "custom_snapshot.json")
	link := filepath.Join(t.TempDir(), "snapshot.json")
	absolute, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(absolute, link); err != nil {
		t.Fatal(err)
	}
	command := newProfileCmd()
	command.SetArgs([]string{"create", "omeka-s", "bad", "--snapshot", link})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("profile create symlink error = %v", err)
	}
}

func TestProfileListAndShowUseCommandWritersAndValidatePair(t *testing.T) {
	useProfileCommandConfig(t)
	snapshot, _ := publishCommandProfile(t, "z-profile")
	publishCommandProfile(t, "a-profile")

	list := newProfileListCmd()
	var listOutput bytes.Buffer
	list.SetOut(&listOutput)
	if err := list.Execute(); err != nil {
		t.Fatalf("profile list Execute() error = %v", err)
	}
	output := listOutput.String()
	if strings.Index(output, "a-profile") > strings.Index(output, "z-profile") || !strings.Contains(output, "MODEL SHA-256") {
		t.Fatalf("profile list output = %q", output)
	}

	show := newProfileShowCmd()
	var showOutput bytes.Buffer
	show.SetOut(&showOutput)
	show.SetArgs([]string{"a-profile"})
	if err := show.Execute(); err != nil {
		t.Fatalf("profile show Execute() error = %v", err)
	}
	shown, err := profile.DecodeDefinition(bytes.NewReader(showOutput.Bytes()))
	if err != nil || shown.Name != "a-profile" {
		t.Fatalf("shown definition = %#v, %v\n%s", shown, err, showOutput.String())
	}

	showModel := newProfileShowCmd()
	var modelOutput bytes.Buffer
	showModel.SetOut(&modelOutput)
	showModel.SetArgs([]string{"a-profile", "--model"})
	if err := showModel.Execute(); err != nil {
		t.Fatalf("profile show --model Execute() error = %v", err)
	}
	shownModel, err := model.Load(bytes.NewReader(modelOutput.Bytes()))
	if err != nil || shownModel.Fingerprint.Value != snapshot.Fingerprint.Value {
		t.Fatalf("shown model = %#v, %v", shownModel, err)
	}
}

func TestProfileCommandRemovesLegacyCSVCreationAndRejectsSymlinkInput(t *testing.T) {
	useProfileCommandConfig(t)
	command := newProfileCmd()
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"create", "csv", "legacy"})
	if err := command.Execute(); err == nil {
		t.Fatal("legacy CSV profile creation remains exposed")
	}

	target := filepath.Join("..", "spec", "testdata", "drupal")
	link := filepath.Join(t.TempDir(), "config")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	create := newProfileCreateDrupalCmd()
	create.SetOut(io.Discard)
	create.SetArgs([]string{"unsafe", "--config", link, "--bundle", "islandora_object"})
	if err := create.Execute(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink profile create error = %v", err)
	}
}

func publishCommandProfile(t *testing.T, name string) (*model.Snapshot, *profile.Definition) {
	t.Helper()
	snapshot, err := compileDrupalModelPath(filepath.Join("..", "spec", "testdata", "drupal"))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := profile.NewDrupalDefinition(snapshot, profile.DrupalDefinitionOptions{
		Name: name, EntityType: "node", Bundle: "islandora_object",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Publish(snapshot, definition, profile.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	return snapshot, definition
}

func useProfileCommandConfig(t *testing.T) {
	t.Helper()
	profile.SetConfigDir(t.TempDir())
	t.Cleanup(func() { profile.SetConfigDir("") })
}

func archiveDirectory(t *testing.T, directory string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		header := &tar.Header{Name: "config/sync/" + entry.Name(), Mode: 0o600, Size: int64(len(data))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstitutionalIdentifierOptionsRequireCompletePolicy(t *testing.T) {
	_, err := institutionalIdentifierOptions("local", "example", "", `^.+$`, "source_record")
	if err == nil || !strings.Contains(err.Error(), "must be supplied together") {
		t.Fatalf("incomplete institution policy error = %v", err)
	}
	_, err = institutionalIdentifierOptions("local", "example", "https://example.edu/id/", `^.+$`, "unknown")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown identity level error = %v", err)
	}
}
