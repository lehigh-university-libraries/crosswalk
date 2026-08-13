package proquest

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBundleStagesPrimaryAndSupplementalFiles(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "delivery.zip")
	writeTestBundle(t, archivePath, map[string][]byte{
		"package/submission_DATA.xml": xmlData,
		"package/item.pdf":            []byte("%PDF-1.7\nprimary"),
		"package/data.csv":            []byte("x,y\n1,2\n"),
	})

	result, err := ReadBundle(context.Background(), archivePath, BundleOptions{MediaDirectory: filepath.Join(directory, "media")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].Title != "Safe ProQuest package" {
		t.Fatalf("Records = %+v", result.Records)
	}
	if len(result.Records[0].Files) != 2 {
		t.Fatalf("Files = %+v", result.Records[0].Files)
	}
	roles := map[string]string{}
	for _, file := range result.Records[0].Files {
		roles[file.Name] = file.Role
		if !strings.HasPrefix(file.Path, result.MediaDirectory+string(filepath.Separator)) {
			t.Errorf("file path %q not within %q", file.Path, result.MediaDirectory)
		}
		if _, err := os.Stat(file.Path); err != nil {
			t.Errorf("staged file %q: %v", file.Path, err)
		}
	}
	if roles["item.pdf"] != "primary" || roles["data.csv"] != "supplemental" {
		t.Fatalf("roles = %#v", roles)
	}
}

func TestReadMetadataValidatesWithoutPublishingMedia(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "delivery.zip")
	writeTestBundle(t, archivePath, map[string][]byte{
		"package/submission_DATA.xml": xmlData,
		"package/item.pdf":            []byte("%PDF-1.7\nprimary"),
		"package/data.csv":            []byte("x,y\n1,2\n"),
	})
	mediaRoot := filepath.Join(directory, "media")

	metadata, err := ReadMetadata(context.Background(), archivePath, BundleOptions{MediaDirectory: mediaRoot})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Record.Title != "Safe ProQuest package" {
		t.Fatalf("record title = %q", metadata.Record.Title)
	}
	if len(metadata.SHA256) != 64 {
		t.Fatalf("SHA256 = %q", metadata.SHA256)
	}
	if _, err := os.Stat(mediaRoot); !os.IsNotExist(err) {
		t.Fatalf("metadata-only read published media directory: %v", err)
	}
}

func TestReadBundleRejectsUnsafeArchivesWithoutPublishingFiles(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		members map[string][]byte
		options BundleOptions
		want    string
	}{
		{
			name: "traversal",
			members: map[string][]byte{
				"submission_DATA.xml": xmlData,
				"../item.pdf":         []byte("%PDF-"),
			},
			want: "unsafe archive member",
		},
		{
			name: "oversized",
			members: map[string][]byte{
				"submission_DATA.xml": xmlData,
				"item.pdf":            []byte("%PDF-too-large"),
			},
			options: BundleOptions{MaxEntryBytes: 5},
			want:    "exceeds 5 bytes",
		},
		{
			name: "missing primary",
			members: map[string][]byte{
				"submission_DATA.xml": xmlData,
				"notes.txt":           []byte("notes"),
			},
			want: "declared primary file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			archivePath := filepath.Join(directory, "delivery.zip")
			writeTestBundle(t, archivePath, test.members)
			test.options.MediaDirectory = filepath.Join(directory, "media")
			_, err := ReadBundle(context.Background(), archivePath, test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadBundle() error = %v, want %q", err, test.want)
			}
			if entries, readErr := os.ReadDir(test.options.MediaDirectory); readErr == nil && len(entries) != 0 {
				t.Fatalf("published entries after failure: %v", entries)
			}
		})
	}
}

func TestReadBundleRejectsExistingPackageDestination(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "delivery.zip")
	writeTestBundle(t, archivePath, map[string][]byte{
		"submission_DATA.xml": xmlData,
		"item.pdf":            []byte("%PDF-"),
	})
	mediaRoot := filepath.Join(directory, "media")
	if _, err := ReadBundle(context.Background(), archivePath, BundleOptions{MediaDirectory: mediaRoot}); err != nil {
		t.Fatal(err)
	}
	_, err = ReadBundle(context.Background(), archivePath, BundleOptions{MediaDirectory: mediaRoot})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("ReadBundle() error = %v", err)
	}
}

func TestReadBundlesRejectsArchiveChangedAfterReconciliation(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "delivery.zip")
	writeTestBundle(t, archivePath, map[string][]byte{
		"submission_DATA.xml": xmlData,
		"item.pdf":            []byte("%PDF-original"),
	})
	metadata, err := ReadMetadata(t.Context(), archivePath, BundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writeTestBundle(t, archivePath, map[string][]byte{
		"submission_DATA.xml": xmlData,
		"item.pdf":            []byte("%PDF-changed"),
	})
	mediaRoot := filepath.Join(directory, "media")
	_, err = ReadBundles(t.Context(), []BundleRequest{{ArchivePath: archivePath, ExpectedSHA256: metadata.SHA256}}, BundleOptions{MediaDirectory: mediaRoot})
	if err == nil || !strings.Contains(err.Error(), "changed after metadata reconciliation") {
		t.Fatalf("ReadBundles() error = %v", err)
	}
	assertNoPublishedBatch(t, mediaRoot)
}

func TestReadBundlesPublishesBatchWithCollidingArchiveBasenames(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archives := make([]string, 0, 2)
	requests := make([]BundleRequest, 0, 2)
	for index, pdf := range [][]byte{[]byte("%PDF-one"), []byte("%PDF-two")} {
		archiveDirectory := filepath.Join(directory, string(rune('a'+index)))
		if err := os.MkdirAll(archiveDirectory, 0o750); err != nil {
			t.Fatal(err)
		}
		archivePath := filepath.Join(archiveDirectory, "delivery.zip")
		writeTestBundle(t, archivePath, map[string][]byte{"submission_DATA.xml": xmlData, "item.pdf": pdf})
		metadata, err := ReadMetadata(t.Context(), archivePath, BundleOptions{})
		if err != nil {
			t.Fatal(err)
		}
		archives = append(archives, archivePath)
		requests = append(requests, BundleRequest{ArchivePath: archivePath, ExpectedSHA256: metadata.SHA256})
	}
	result, err := ReadBundles(t.Context(), requests, BundleOptions{MediaDirectory: filepath.Join(directory, "media")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != len(archives) || len(result.Files) != len(archives) {
		t.Fatalf("result = %+v", result)
	}
	if filepath.Dir(result.Files[0]) == filepath.Dir(result.Files[1]) {
		t.Fatalf("colliding archive basenames shared package directory: %v", result.Files)
	}
}

func TestReadBundlesDoesNotPublishPartialBatch(t *testing.T) {
	t.Parallel()
	xmlData, err := os.ReadFile(filepath.Join("testdata", "submission_DATA.xml"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "delivery.zip")
	writeTestBundle(t, archivePath, map[string][]byte{"submission_DATA.xml": xmlData, "item.pdf": []byte("%PDF-")})
	metadata, err := ReadMetadata(t.Context(), archivePath, BundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mediaRoot := filepath.Join(directory, "media")
	_, err = ReadBundles(t.Context(), []BundleRequest{
		{ArchivePath: archivePath, ExpectedSHA256: metadata.SHA256},
		{ArchivePath: archivePath, ExpectedSHA256: strings.Repeat("0", 64)},
	}, BundleOptions{MediaDirectory: mediaRoot})
	if err == nil {
		t.Fatal("ReadBundles() error = nil")
	}
	assertNoPublishedBatch(t, mediaRoot)
}

func TestDiscardBatchIsScopedToMediaRoot(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	mediaRoot := filepath.Join(directory, "media")
	batch := filepath.Join(mediaRoot, "proquest-0123456789abcdef")
	if err := os.MkdirAll(batch, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := DiscardBatch(mediaRoot, BundleResult{MediaDirectory: batch}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(batch); !os.IsNotExist(err) {
		t.Fatalf("batch still exists: %v", err)
	}
	outside := filepath.Join(directory, "proquest-0123456789abcdef")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := DiscardBatch(mediaRoot, BundleResult{MediaDirectory: outside}); err == nil {
		t.Fatal("DiscardBatch(outside) error = nil")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside path was removed: %v", err)
	}
}

func assertNoPublishedBatch(t *testing.T, mediaRoot string) {
	t.Helper()
	entries, err := os.ReadDir(mediaRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "proquest-") {
			t.Fatalf("published batch after failure: %s", entry.Name())
		}
	}
}

func writeTestBundle(t *testing.T, archivePath string, members map[string][]byte) {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(members[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
