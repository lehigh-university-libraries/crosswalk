// Package localfs provides a confined, read-only filesystem validation
// capability for Workbench staging files.
package localfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/validationcontext"
)

const (
	maxConfiguredRoots = 64
	maxPathBytes       = 4096
)

// Resolver checks files beneath an immutable set of explicit absolute roots.
// It is safe for concurrent use.
type Resolver struct {
	roots []string
}

var _ validationcontext.FileReadabilityResolver = (*Resolver)(nil)

// New validates and copies the allowed filesystem roots. Each root must be a
// clean absolute path beneath (and not equal to) its filesystem volume root.
// Root existence and readability are evaluated by FileReadable so a missing
// staging mount produces a validation finding rather than a startup failure.
func New(roots []string) (*Resolver, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("at least one file validation root is required")
	}
	if len(roots) > maxConfiguredRoots {
		return nil, fmt.Errorf("file validation roots exceed the limit of %d", maxConfiguredRoots)
	}

	configured := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if err := validateAbsolutePath(root, "file validation root"); err != nil {
			return nil, err
		}
		if root == filesystemAnchor(root) {
			return nil, fmt.Errorf("file validation root must not authorize a filesystem volume root")
		}
		if _, duplicate := seen[root]; duplicate {
			continue
		}
		seen[root] = struct{}{}
		configured = append(configured, root)
	}

	// Prefer the narrowest applicable authorization when roots overlap. The
	// order is deterministic so behavior cannot depend on configuration map
	// iteration in callers.
	sort.Slice(configured, func(i, j int) bool {
		if len(configured[i]) == len(configured[j]) {
			return configured[i] < configured[j]
		}
		return len(configured[i]) > len(configured[j])
	})
	return &Resolver{roots: configured}, nil
}

// FileReadable reports whether normalizedPath is a readable regular file
// beneath an allowed root. Every directory component is opened relative to an
// already-confined directory handle. Symlinks are rejected, and file identity
// is compared before and after each open to close rename/symlink race windows.
func (r *Resolver) FileReadable(ctx context.Context, normalizedPath string) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("checking staged file: context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("checking staged file: %w", err)
	}
	if r == nil || len(r.roots) == 0 {
		return false, fmt.Errorf("checking staged file: no roots are configured")
	}
	if err := validateAbsolutePath(normalizedPath, "staged file path"); err != nil {
		return false, err
	}

	root, relative, ok := r.matchingRoot(normalizedPath)
	if !ok {
		return false, fmt.Errorf("staged file path is outside configured roots")
	}
	if relative == "." {
		return false, nil
	}

	current, available, err := openRootWithoutSymlinks(ctx, root)
	if err != nil || !available {
		return false, err
	}
	defer func() { _ = current.Close() }()

	components := strings.Split(relative, string(filepath.Separator))
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("checking staged file: %w", err)
		}
		info, err := current.Lstat(component)
		if unavailablePathError(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("checking staged file component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("staged file path contains a symbolic link")
		}

		if index == len(components)-1 {
			if !info.Mode().IsRegular() {
				return false, nil
			}
			file, err := current.Open(component)
			if unavailablePathError(err) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("opening staged file: %w", err)
			}
			openedInfo, statErr := file.Stat()
			closeErr := file.Close()
			if statErr != nil {
				return false, fmt.Errorf("checking opened staged file: %w", statErr)
			}
			if closeErr != nil {
				return false, fmt.Errorf("closing staged file: %w", closeErr)
			}
			if !os.SameFile(info, openedInfo) {
				return false, fmt.Errorf("staged file changed while it was being checked")
			}
			if !openedInfo.Mode().IsRegular() {
				return false, nil
			}
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("checking staged file: %w", err)
			}
			return true, nil
		}

		if !info.IsDir() {
			return false, nil
		}
		next, err := current.OpenRoot(component)
		if unavailablePathError(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("opening staged file directory: %w", err)
		}
		openedInfo, err := next.Stat(".")
		if err != nil {
			_ = next.Close()
			return false, fmt.Errorf("checking staged file directory: %w", err)
		}
		if !os.SameFile(info, openedInfo) || !openedInfo.IsDir() {
			_ = next.Close()
			return false, fmt.Errorf("staged file directory changed while it was being checked")
		}
		_ = current.Close()
		current = next
	}

	return false, nil
}

func (r *Resolver) matchingRoot(path string) (string, string, bool) {
	for _, root := range r.roots {
		relative, err := filepath.Rel(root, path)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		return root, relative, true
	}
	return "", "", false
}

func openRootWithoutSymlinks(ctx context.Context, path string) (*os.Root, bool, error) {
	anchor := filesystemAnchor(path)
	current, err := os.OpenRoot(anchor)
	if unavailablePathError(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("opening filesystem volume root: %w", err)
	}

	relative, err := filepath.Rel(anchor, path)
	if err != nil {
		_ = current.Close()
		return nil, false, fmt.Errorf("locating configured file root: %w", err)
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if err := ctx.Err(); err != nil {
			_ = current.Close()
			return nil, false, fmt.Errorf("opening configured file root: %w", err)
		}
		info, err := current.Lstat(component)
		if unavailablePathError(err) {
			_ = current.Close()
			return nil, false, nil
		}
		if err != nil {
			_ = current.Close()
			return nil, false, fmt.Errorf("checking configured file root: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = current.Close()
			return nil, false, fmt.Errorf("configured file root contains a symbolic link")
		}
		if !info.IsDir() {
			_ = current.Close()
			return nil, false, nil
		}

		next, err := current.OpenRoot(component)
		if unavailablePathError(err) {
			_ = current.Close()
			return nil, false, nil
		}
		if err != nil {
			_ = current.Close()
			return nil, false, fmt.Errorf("opening configured file root: %w", err)
		}
		openedInfo, err := next.Stat(".")
		if err != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, false, fmt.Errorf("checking opened file root: %w", err)
		}
		if !os.SameFile(info, openedInfo) || !openedInfo.IsDir() {
			_ = next.Close()
			_ = current.Close()
			return nil, false, fmt.Errorf("configured file root changed while it was being opened")
		}
		_ = current.Close()
		current = next
	}
	return current, true, nil
}

func validateAbsolutePath(path, description string) error {
	if path == "" || len(path) > maxPathBytes || strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("%s must be a bounded clean absolute path", description)
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%s must be a clean absolute path", description)
	}
	return nil
}

func filesystemAnchor(path string) string {
	volume := filepath.VolumeName(path)
	return volume + string(filepath.Separator)
}

func unavailablePathError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission)
}
