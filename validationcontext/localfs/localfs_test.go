package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestResolverChecksReadableRegularFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "objects")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "regular file", path: file, want: true},
		{name: "missing file", path: filepath.Join(directory, "missing.pdf")},
		{name: "directory", path: directory},
		{name: "root directory", path: root},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolver.FileReadable(context.Background(), test.path)
			if err != nil {
				t.Fatalf("FileReadable() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("FileReadable() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestResolverRejectsSymlinksAtEveryLevel(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "staging")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}

	linkedDirectory := filepath.Join(root, "linked-directory")
	if err := os.Symlink(outside, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	linkedFile := filepath.Join(root, "linked-file")
	if err := os.Symlink(outsideFile, linkedFile); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(linkedDirectory, "secret.txt"), linkedFile} {
		if ok, err := resolver.FileReadable(context.Background(), path); ok || err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Errorf("FileReadable(%q) = (%t, %v), want symlink rejection", path, ok, err)
		}
	}

	linkedRoot := filepath.Join(parent, "linked-root")
	if err := os.Symlink(root, linkedRoot); err != nil {
		t.Fatal(err)
	}
	rootResolver, err := New([]string{linkedRoot})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rootResolver.FileReadable(context.Background(), filepath.Join(linkedRoot, "anything")); ok || err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("FileReadable() = (%t, %v), want configured-root symlink rejection", ok, err)
	}
}

func TestResolverRejectsPathsOutsideItsExactRoots(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "staging")
	sibling := filepath.Join(parent, "staging-other")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sibling, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := resolver.FileReadable(context.Background(), file); ok || err == nil || !strings.Contains(err.Error(), "outside configured roots") {
		t.Fatalf("FileReadable() = (%t, %v), want outside-root rejection", ok, err)
	}
}

func TestResolverRejectsInvalidConfigurationAndCandidatePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name  string
		roots []string
	}{
		{name: "missing roots"},
		{name: "relative root", roots: []string{"staging"}},
		{name: "unclean root", roots: []string{root + string(filepath.Separator) + "."}},
		{name: "volume root", roots: []string{filesystemAnchor(root)}},
		{name: "too many roots", roots: repeatRootPaths(root, maxConfiguredRoots+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.roots); err == nil {
				t.Fatal("New() error = nil, want configuration rejection")
			}
		})
	}

	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative/file", root + string(filepath.Separator) + "directory" + string(filepath.Separator) + ".." + string(filepath.Separator) + "file", root + "\nfile", "/" + strings.Repeat("a", maxPathBytes)} {
		if ok, err := resolver.FileReadable(context.Background(), path); ok || err == nil {
			t.Errorf("FileReadable(%q) = (%t, %v), want invalid-path rejection", path, ok, err)
		}
	}
}

func TestResolverDeduplicatesIdenticalRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New([]string{root, root})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := resolver.FileReadable(context.Background(), file); err != nil || !ok {
		t.Fatalf("FileReadable() = (%t, %v), want (true, nil)", ok, err)
	}
}

func TestResolverHonorsCancellationAndSupportsConcurrentUse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := resolver.FileReadable(ctx, file); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("FileReadable(canceled) = (%t, %v), want context.Canceled", ok, err)
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ok, err := resolver.FileReadable(context.Background(), file); err != nil || !ok {
				t.Errorf("FileReadable() = (%t, %v), want (true, nil)", ok, err)
			}
		}()
	}
	wait.Wait()
}

func TestResolverTreatsUnreadableFilesAsUnavailable(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })

	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := resolver.FileReadable(context.Background(), file); err != nil || ok {
		t.Fatalf("FileReadable() = (%t, %v), want (false, nil)", ok, err)
	}
}

func TestResolverTreatsFilesUnderUnreadableDirectoriesAsUnavailable(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "restricted")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "object.pdf")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := resolver.FileReadable(context.Background(), file); err != nil || ok {
		t.Fatalf("FileReadable() = (%t, %v), want (false, nil)", ok, err)
	}
}

func TestResolverTreatsMissingConfiguredRootAsUnavailable(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "not-mounted")
	resolver, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := resolver.FileReadable(context.Background(), filepath.Join(root, "object.pdf")); err != nil || ok {
		t.Fatalf("FileReadable() = (%t, %v), want (false, nil)", ok, err)
	}
}

func repeatRootPaths(root string, count int) []string {
	paths := make([]string, count)
	for index := range paths {
		paths[index] = filepath.Join(root, "root-"+strings.Repeat("x", index+1))
	}
	return paths
}
