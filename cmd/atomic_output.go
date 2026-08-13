package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func writeOutputFile(path string, write func(io.Writer) error) (err error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || strings.TrimSpace(path) == "" {
		return fmt.Errorf("resolving output path: output path is required")
	}
	directory := filepath.Dir(absolute)
	temporary, err := os.CreateTemp(directory, ".crosswalk-output-*")
	if err != nil {
		return fmt.Errorf("creating temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		var cleanupErrors []error
		if temporary != nil {
			if closeErr := temporary.Close(); closeErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("closing temporary output: %w", closeErr))
			}
		}
		if !keep {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("removing temporary output: %w", removeErr))
			}
		}
		if len(cleanupErrors) > 0 {
			err = errors.Join(append([]error{err}, cleanupErrors...)...)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return fmt.Errorf("setting temporary output permissions: %w", err)
	}
	if err := write(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("syncing temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("closing temporary output: %w", err)
	}
	temporary = nil
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return fmt.Errorf("publishing output: %w", err)
	}
	keep = true
	return nil
}

// writeNewFile stages complete bytes beside the destination, then publishes
// them with atomic no-clobber semantics. Concurrent writers cannot replace one
// another and readers never observe a partially written report.
func writeNewFile(path string, data []byte) (err error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || strings.TrimSpace(path) == "" {
		return fmt.Errorf("resolving new output path: output path is required")
	}
	temporary, err := os.CreateTemp(filepath.Dir(absolute), ".crosswalk-new-output-*")
	if err != nil {
		return fmt.Errorf("creating temporary new output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if temporary != nil {
			err = errors.Join(err, temporary.Close())
		}
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("removing temporary new output: %w", removeErr))
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return fmt.Errorf("setting temporary new output permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("writing temporary new output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("syncing temporary new output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("closing temporary new output: %w", err)
	}
	temporary = nil
	if err := os.Link(temporaryPath, absolute); err != nil {
		return fmt.Errorf("publishing new output: %w", err)
	}
	return nil
}

type namedOutput struct {
	name string
	data []byte
}

func writeOutputDirectory(destination string, outputs []namedOutput) (err error) {
	absolute, err := filepath.Abs(strings.TrimSpace(destination))
	if err != nil || strings.TrimSpace(destination) == "" {
		return fmt.Errorf("resolving output directory: output directory is required")
	}
	if _, err := os.Lstat(absolute); err == nil {
		return fmt.Errorf("output directory already exists: %s", absolute)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking output directory: %w", err)
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("creating output parent directory: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".crosswalk-artifacts-*")
	if err != nil {
		return fmt.Errorf("creating output workspace: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			if removeErr := os.RemoveAll(temporary); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("removing output workspace: %w", removeErr))
			}
		}
	}()
	seen := make(map[string]struct{}, len(outputs))
	for _, output := range outputs {
		if output.name == "" || filepath.Base(output.name) != output.name {
			return fmt.Errorf("invalid output name %q", output.name)
		}
		if _, exists := seen[output.name]; exists {
			return fmt.Errorf("duplicate output name %q", output.name)
		}
		seen[output.name] = struct{}{}
		path := filepath.Join(temporary, output.name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
		if err != nil {
			return fmt.Errorf("creating output %q: %w", path, err)
		}
		if _, writeErr := file.Write(output.data); writeErr != nil {
			closeErr := file.Close()
			return errors.Join(fmt.Errorf("writing output %q: %w", path, writeErr), closeErr)
		}
		if err := file.Sync(); err != nil {
			closeErr := file.Close()
			return errors.Join(fmt.Errorf("syncing output %q: %w", path, err), closeErr)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("closing output %q: %w", path, err)
		}
	}
	if err := os.Rename(temporary, absolute); err != nil {
		return fmt.Errorf("publishing output directory: %w", err)
	}
	keep = true
	return nil
}
