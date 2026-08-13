package profile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/model"
	"gopkg.in/yaml.v3"
)

const (
	maxStoredModelBytes   = int64(64 << 20)
	maxStoredProfileBytes = int64(64 << 20)
)

// PublishOptions controls canonical profile publication.
type PublishOptions struct {
	Force bool
}

// StoredProfile is a definition compiled against the immutable model snapshot
// it names.
type StoredProfile struct {
	Definition *Definition
	Model      *model.Snapshot
	Compiled   *Compiled
}

// Publish validates and publishes a model/profile pair. Models are immutable
// content-addressed objects and are written first. The profile rename is the
// commit point, so a failed publication cannot expose a profile without its
// exact model. A failure may leave an unreferenced model object, which is safe
// to retain and reuse.
func Publish(snapshot *model.Snapshot, definition *Definition, options PublishOptions) (*Compiled, error) {
	compiled, err := Compile(snapshot, definition)
	if err != nil {
		return nil, fmt.Errorf("publishing profile: %w", err)
	}
	if err := EnsureProfilesDir(); err != nil {
		return nil, fmt.Errorf("creating profiles directory: %w", err)
	}
	profilePath, err := ProfilePath(definition.Name)
	if err != nil {
		return nil, err
	}
	if !options.Force {
		exists, err := regularFileExists(profilePath, "profile")
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("profile %q already exists; use --force to overwrite", definition.Name)
		}
	}

	if err := StoreModel(snapshot); err != nil {
		return nil, fmt.Errorf("storing profile model: %w", err)
	}
	data, err := yaml.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("marshaling profile definition: %w", err)
	}
	if err := writeFileAtomicMode(profilePath, data, 0o644, options.Force); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("profile %q already exists; use --force to overwrite", definition.Name)
		}
		return nil, fmt.Errorf("publishing profile definition: %w", err)
	}
	return compiled, nil
}

// LoadStored loads a canonical definition, resolves its content-addressed
// model, and recompiles the pair to detect drift or corruption.
func LoadStored(name string) (*StoredProfile, error) {
	definition, err := LoadDefinition(name)
	if err != nil {
		return nil, err
	}
	snapshot, err := LoadModel(definition.ModelFingerprint)
	if err != nil {
		return nil, fmt.Errorf("loading model for profile %q: %w", name, err)
	}
	compiled, err := Compile(snapshot, definition)
	if err != nil {
		return nil, fmt.Errorf("loading profile %q: %w", name, err)
	}
	return &StoredProfile{Definition: definition, Model: snapshot, Compiled: compiled}, nil
}

// LoadDefinition reads one canonical profile definition by its strict name.
func LoadDefinition(name string) (_ *Definition, returnErr error) {
	path, err := existingProfilePath(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("profile definition %q not found", name)
		}
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("profile definition %q not found", name)
		}
		return nil, fmt.Errorf("opening profile definition: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("closing profile definition: %w", closeErr))
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxStoredProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading profile definition: %w", err)
	}
	if int64(len(data)) > maxStoredProfileBytes {
		return nil, fmt.Errorf("stored profile exceeds %d bytes", maxStoredProfileBytes)
	}
	definition, err := DecodeDefinition(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if definition.Name != name {
		return nil, fmt.Errorf("profile definition %q contains mismatched name %q", name, definition.Name)
	}
	return definition, nil
}

// ListDefinitions returns canonical profile names in lexical order.
func ListDefinitions() ([]string, error) {
	directory, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	if err := validateProfilesStorage(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil, fmt.Errorf("profile %q is not a regular file", entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := validateProfileName(name); err != nil {
			return nil, fmt.Errorf("invalid stored profile filename %q: %w", entry.Name(), err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// DeleteDefinition removes a canonical profile definition. Immutable model
// objects are deliberately retained because other profiles may reference them.
func DeleteDefinition(name string) error {
	path, err := existingProfilePath(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile definition %q not found", name)
		}
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile definition %q not found", name)
		}
		return fmt.Errorf("deleting profile definition: %w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("syncing profiles directory: %w", err)
	}
	return nil
}

// DefinitionExists reports whether a canonical profile definition exists.
func DefinitionExists(name string) (bool, error) {
	path, err := ProfilePath(name)
	if err != nil {
		return false, err
	}
	if err := validateProfilesStorage(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return regularFileExists(path, "profile")
}

// ModelsDir returns the immutable SHA-256 model object directory.
func ModelsDir() (string, error) {
	configDirectory, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirectory, "models", "sha256"), nil
}

// EnsureModelsDir safely creates the immutable model object directory.
func EnsureModelsDir() error {
	configDirectory, err := ConfigDir()
	if err != nil {
		return err
	}
	modelsDirectory, err := ModelsDir()
	if err != nil {
		return err
	}
	return ensureStorageDirectories(configDirectory, filepath.Join(configDirectory, "models"), modelsDirectory)
}

// ModelPath returns the path for an immutable model object.
func ModelPath(fingerprint string) (string, error) {
	if err := validateSHA256("model fingerprint", fingerprint); err != nil {
		return "", err
	}
	directory, err := ModelsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Clean(directory), fingerprint+".yaml")
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return "", fmt.Errorf("resolving model path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("model path escapes models directory")
	}
	return path, nil
}

// StoreModel writes an immutable model object if it is not already present.
// Acquisition provenance is intentionally omitted because it does not
// participate in the model fingerprint: two sites with the same schema must
// produce identical bytes at the same content address. An existing object is
// loaded and validated rather than overwritten.
func StoreModel(snapshot *model.Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("model snapshot is nil")
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if err := EnsureModelsDir(); err != nil {
		return fmt.Errorf("creating models directory: %w", err)
	}
	path, err := ModelPath(snapshot.Fingerprint.Value)
	if err != nil {
		return err
	}
	exists, err := regularFileExists(path, "model")
	if err != nil {
		return err
	}
	if exists {
		_, err := LoadModel(snapshot.Fingerprint.Value)
		return err
	}
	canonical, err := snapshot.Canonical()
	if err != nil {
		return fmt.Errorf("canonicalizing model snapshot: %w", err)
	}
	canonical.Provenance = model.Provenance{}
	data, err := yaml.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("marshaling model snapshot: %w", err)
	}
	if err := writeFileAtomicMode(path, data, 0o644, false); err != nil {
		if errors.Is(err, os.ErrExist) {
			_, loadErr := LoadModel(snapshot.Fingerprint.Value)
			return loadErr
		}
		return fmt.Errorf("writing model snapshot: %w", err)
	}
	return nil
}

// LoadModel loads a model object and verifies that both its recorded and
// computed fingerprints match its content-addressed filename.
func LoadModel(fingerprint string) (_ *model.Snapshot, returnErr error) {
	path, err := ModelPath(fingerprint)
	if err != nil {
		return nil, err
	}
	if err := validateModelsStorage(); err != nil {
		return nil, err
	}
	exists, err := regularFileExists(path, "model")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("model %q not found", fingerprint)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening model snapshot: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("closing model snapshot: %w", closeErr))
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxStoredModelBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading model snapshot: %w", err)
	}
	if int64(len(data)) > maxStoredModelBytes {
		return nil, fmt.Errorf("stored model exceeds %d bytes", maxStoredModelBytes)
	}
	snapshot, err := model.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if snapshot.Fingerprint.Value != fingerprint {
		return nil, fmt.Errorf("stored model fingerprint %s does not match object name %s", snapshot.Fingerprint.Value, fingerprint)
	}
	if snapshot.Provenance != (model.Provenance{}) {
		return nil, fmt.Errorf("stored model %s contains non-addressed acquisition provenance", fingerprint)
	}
	return snapshot, nil
}

func existingProfilePath(name string) (string, error) {
	path, err := ProfilePath(name)
	if err != nil {
		return "", err
	}
	if err := validateProfilesStorage(); err != nil {
		return "", err
	}
	exists, err := regularFileExists(path, "profile")
	if err != nil {
		return "", err
	}
	if !exists {
		return "", os.ErrNotExist
	}
	return path, nil
}

func validateProfilesStorage() error {
	configDirectory, err := ConfigDir()
	if err != nil {
		return err
	}
	profilesDirectory, err := ProfilesDir()
	if err != nil {
		return err
	}
	for _, directory := range []string{configDirectory, profilesDirectory} {
		if err := validateStorageDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func validateModelsStorage() error {
	configDirectory, err := ConfigDir()
	if err != nil {
		return err
	}
	modelsDirectory, err := ModelsDir()
	if err != nil {
		return err
	}
	for _, directory := range []string{configDirectory, filepath.Join(configDirectory, "models"), modelsDirectory} {
		if err := validateStorageDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func validateStorageDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("storage path %q is not a regular directory", directory)
	}
	return nil
}

func ensureStorageDirectories(directories ...string) error {
	for index, directory := range directories {
		var err error
		if index == 0 {
			err = os.MkdirAll(directory, 0o755)
		} else {
			err = os.Mkdir(directory, 0o755)
		}
		if err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("creating storage directory %q: %w", directory, err)
		}
		if err := validateStorageDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func regularFileExists(path, label string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking %s %q: %w", label, path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s %q is not a regular file", label, path)
	}
	return true, nil
}

func writeFileAtomicMode(path string, data []byte, mode os.FileMode, replace bool) (returnErr error) {
	directory := filepath.Dir(path)
	if err := validateStorageDirectory(directory); err != nil {
		return err
	}
	if replace {
		if _, err := regularFileExists(path, "destination"); err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(directory, ".crosswalk-profile-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if temporary != nil {
			if closeErr := temporary.Close(); closeErr != nil {
				returnErr = errors.Join(returnErr, closeErr)
			}
		}
		if removeTemporary {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				returnErr = errors.Join(returnErr, removeErr)
			}
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return err
	}
	temporary = nil
	if replace {
		if err := os.Rename(temporaryPath, path); err != nil {
			return err
		}
		removeTemporary = false
	} else {
		if err := os.Link(temporaryPath, path); err != nil {
			return err
		}
		if err := os.Remove(temporaryPath); err != nil {
			return err
		}
		removeTemporary = false
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) (returnErr error) {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := handle.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, closeErr)
		}
	}()
	return handle.Sync()
}
