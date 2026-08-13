package spec

import (
	"fmt"
	"path"
	"strings"
)

// FileStagingRootDefault is the transformation default that supplies the
// absolute Workbench staging directory for relative media paths.
const FileStagingRootDefault = "file.staging_root"

// FileAllowedAbsoluteRootsDefault is the transformation default containing a
// pipe-delimited allowlist of absolute media roots that may pass through.
const FileAllowedAbsoluteRootsDefault = "file.allowed_absolute_roots"

// NormalizeFilePath converts a source media reference to the path written to
// Islandora Workbench. Allowlisted POSIX absolute paths stay absolute. Other
// leading-slash and relative paths are cleaned and rooted under
// file.staging_root. URLs are not Workbench mounted-file paths and are
// rejected.
func (t *Transformation) NormalizeFilePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("file path contains a control character")
	}
	if strings.HasPrefix(value, `\\`) {
		return "", fmt.Errorf("UNC file paths are not supported: %q", value)
	}

	normalized := strings.ReplaceAll(value, `\`, "/")
	if isWindowsDrivePath(normalized) {
		return "", fmt.Errorf("Windows drive file paths are not supported: %q", value)
	}
	if hasURIScheme(normalized) {
		return "", fmt.Errorf("file URLs are not supported: %q", value)
	}
	if path.IsAbs(normalized) {
		absolute := path.Clean(normalized)
		roots, err := fileAllowedAbsoluteRoots(t.Default(FileAllowedAbsoluteRootsDefault))
		if err != nil {
			return "", err
		}
		for _, root := range roots {
			if pathWithinRoot(absolute, root) {
				return absolute, nil
			}
		}
		normalized = strings.TrimLeft(absolute, "/")
	}

	relative := path.Clean(normalized)
	if relative == "." {
		return "", fmt.Errorf("file path is empty after cleaning")
	}
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("relative file path escapes %s: %q", FileStagingRootDefault, value)
	}

	root, err := fileStagingRoot(t.Default(FileStagingRootDefault))
	if err != nil {
		return "", err
	}
	joined := path.Join(root, relative)
	if root != "/" && !strings.HasPrefix(joined, root+"/") {
		return "", fmt.Errorf("relative file path escapes %s: %q", FileStagingRootDefault, value)
	}
	return joined, nil
}

func validateFileDefaults(defaults map[string]string) error {
	root, configured := defaults[FileStagingRootDefault]
	if configured {
		if _, err := fileStagingRoot(root); err != nil {
			return fmt.Errorf("invalid %s default: %w", FileStagingRootDefault, err)
		}
	}
	allowedRoots, configured := defaults[FileAllowedAbsoluteRootsDefault]
	if configured {
		if _, err := fileAllowedAbsoluteRoots(allowedRoots); err != nil {
			return fmt.Errorf("invalid %s default: %w", FileAllowedAbsoluteRootsDefault, err)
		}
	}
	return nil
}

func fileStagingRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("relative file paths require a non-empty %s default", FileStagingRootDefault)
	}
	if strings.ContainsAny(value, "\x00\r\n") || strings.Contains(value, `\`) {
		return "", fmt.Errorf("staging root must be an absolute POSIX path")
	}
	cleaned := path.Clean(value)
	canonical := strings.TrimRight(value, "/")
	if canonical == "" {
		canonical = "/"
	}
	if !path.IsAbs(value) || cleaned != canonical {
		return "", fmt.Errorf("staging root must be a clean absolute POSIX path")
	}
	if cleaned == "/" {
		return "", fmt.Errorf("staging root must not authorize the filesystem root")
	}
	return cleaned, nil
}

func isWindowsDrivePath(value string) bool {
	if len(value) < 3 || value[1] != ':' || value[2] != '/' {
		return false
	}
	return value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z'
}

func fileAllowedAbsoluteRoots(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, "|")
	roots := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("allowed absolute roots cannot contain an empty entry")
		}
		root, err := fileStagingRoot(part)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[root]; exists {
			return nil, fmt.Errorf("allowed absolute root %q is duplicated", root)
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots, nil
}

func pathWithinRoot(value, root string) bool {
	return root == "/" || value == root || strings.HasPrefix(value, root+"/")
}

func hasURIScheme(value string) bool {
	colon := strings.IndexByte(value, ':')
	if colon < 1 || !isASCIIAlpha(value[0]) {
		return false
	}
	for index := 1; index < colon; index++ {
		character := value[index]
		if isASCIIAlpha(character) || character >= '0' && character <= '9' || character == '+' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
