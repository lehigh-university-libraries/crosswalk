package spec

import (
	"strings"
	"testing"
)

func TestNormalizeFilePath(t *testing.T) {
	transformation := FabricatorWorkbench()
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "relative", value: "nested/file.pdf", want: "/mnt/islandora_staging/nested/file.pdf"},
		{name: "backslashes", value: `nested\file.pdf`, want: "/mnt/islandora_staging/nested/file.pdf"},
		{name: "clean relative", value: "nested/drafts/../file.pdf", want: "/mnt/islandora_staging/nested/file.pdf"},
		{name: "staging absolute", value: "/mnt/islandora_staging/nested/file.pdf", want: "/mnt/islandora_staging/nested/file.pdf"},
		{name: "other absolute", value: "/home/import/file.pdf", want: "/home/import/file.pdf"},
		{name: "other mounted absolute", value: "/mnt/import/file.pdf", want: "/mnt/import/file.pdf"},
		{name: "disallowed absolute becomes staged", value: "/etc/passwd", want: "/mnt/islandora_staging/etc/passwd"},
		{name: "absolute backslashes", value: `\mnt\islandora_staging\file.pdf`, want: "/mnt/islandora_staging/file.pdf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := transformation.NormalizeFilePath(test.value)
			if err != nil {
				t.Fatalf("NormalizeFilePath() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizeFilePath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeFilePathRejectsEscapesAndUnsupportedReferences(t *testing.T) {
	transformation := FabricatorWorkbench()
	for _, value := range []string{
		"../private.pdf",
		"nested/../../private.pdf",
		`\\server\share\private.pdf`,
		`C:\private.pdf`,
		"http://example.edu/private.pdf",
		"https://example.edu/private.pdf",
		"ftp://example.edu/private.pdf",
		"https:/example.edu/private.pdf",
		"private.pdf\nother.pdf",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := transformation.NormalizeFilePath(value); err == nil {
				t.Fatalf("NormalizeFilePath(%q) error = nil", value)
			}
		})
	}
}

func TestNormalizeFilePathRequiresExplicitRootOnlyForRelativePaths(t *testing.T) {
	transformation := FabricatorWorkbench()
	delete(transformation.Defaults, FileStagingRootDefault)
	transformation.Fingerprint = Fingerprint{}

	if got, err := transformation.NormalizeFilePath("/home/import/file.pdf"); err != nil || got != "/home/import/file.pdf" {
		t.Fatalf("absolute NormalizeFilePath() = %q, %v", got, err)
	}
	if _, err := transformation.NormalizeFilePath("relative/file.pdf"); err == nil || !strings.Contains(err.Error(), FileStagingRootDefault) {
		t.Fatalf("relative NormalizeFilePath() error = %v", err)
	}
}

func TestValidateRejectsUnsafeFileStagingRoot(t *testing.T) {
	for _, root := range []string{"relative", "/mnt/staging/../private", `C:\staging`, "/"} {
		t.Run(root, func(t *testing.T) {
			transformation := FabricatorWorkbench()
			transformation.Defaults[FileStagingRootDefault] = root
			transformation.Fingerprint = Fingerprint{}
			if err := transformation.Validate(); err == nil || !strings.Contains(err.Error(), FileStagingRootDefault) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsUnsafeAllowedAbsoluteRoots(t *testing.T) {
	for _, roots := range []string{"relative|/mnt", "/home||/mnt", "/home|/home", "/home/../etc|/mnt", "/|/mnt"} {
		t.Run(roots, func(t *testing.T) {
			transformation := FabricatorWorkbench()
			transformation.Defaults[FileAllowedAbsoluteRootsDefault] = roots
			transformation.Fingerprint = Fingerprint{}
			if err := transformation.Validate(); err == nil || !strings.Contains(err.Error(), FileAllowedAbsoluteRootsDefault) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}
