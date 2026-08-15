// Package profile defines strict, model-bound metadata profiles.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// configDirOverride holds a user-specified configuration directory.
// When empty, the default $HOME/.crosswalk is used.
var configDirOverride string
var configDirMutex sync.RWMutex

// SetConfigDir overrides the default configuration directory for the process.
// It exists for CLI startup configuration and tests: set it before concurrent
// work begins and keep it stable until that work completes. The mutex prevents
// data races on the override itself, but changing it during storage operations
// can make one logical operation observe more than one directory.
func SetConfigDir(dir string) {
	configDirMutex.Lock()
	defer configDirMutex.Unlock()
	configDirOverride = dir
}

// ConfigDir returns the crosswalk configuration directory.
func ConfigDir() (string, error) {
	configDirMutex.RLock()
	override := configDirOverride
	configDirMutex.RUnlock()
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".crosswalk"), nil
}

// ProfilesDir returns the profiles directory.
func ProfilesDir() (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "profiles"), nil
}

// EnsureProfilesDir creates the profiles directory if it doesn't exist.
func EnsureProfilesDir() error {
	configDirectory, err := ConfigDir()
	if err != nil {
		return err
	}
	directory, err := ProfilesDir()
	if err != nil {
		return err
	}
	return ensureStorageDirectories(configDirectory, directory)
}

// ProfilePath returns the path for a profile file.
func ProfilePath(name string) (string, error) {
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Clean(dir)
	profilePath := filepath.Join(dir, name+".yaml")
	relative, err := filepath.Rel(dir, profilePath)
	if err != nil {
		return "", fmt.Errorf("resolving profile path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("profile path escapes profiles directory")
	}
	return profilePath, nil
}
