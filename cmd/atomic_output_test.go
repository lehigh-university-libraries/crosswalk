package cmd

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const removedWorkingDirectoryEnv = "CROSSWALK_TEST_REMOVED_WORKING_DIRECTORY"

func TestAtomicOutputsRejectBlankPath(t *testing.T) {
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "replaceable file",
			call: func() error { return writeOutputFile(" \t", func(io.Writer) error { return nil }) },
			want: "resolving output path: output path is required",
		},
		{
			name: "new file",
			call: func() error { return writeNewFile("\n", nil) },
			want: "resolving new output path: output path is required",
		},
		{
			name: "directory",
			call: func() error { return writeOutputDirectory("  ", nil) },
			want: "resolving output directory: output directory is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAtomicOutputsPreserveAbsolutePathErrors(t *testing.T) {
	if directory := os.Getenv(removedWorkingDirectoryEnv); directory != "" {
		if err := os.Chdir(directory); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(directory); err != nil {
			t.Fatal(err)
		}
		tests := []struct {
			name       string
			call       func() error
			wantPrefix string
		}{
			{
				name:       "replaceable file",
				call:       func() error { return writeOutputFile("result.csv", func(io.Writer) error { return nil }) },
				wantPrefix: "resolving output path:",
			},
			{
				name:       "new file",
				call:       func() error { return writeNewFile("result.csv", nil) },
				wantPrefix: "resolving new output path:",
			},
			{
				name:       "directory",
				call:       func() error { return writeOutputDirectory("artifacts", nil) },
				wantPrefix: "resolving output directory:",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				err := test.call()
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("error = %v, want wrapped working-directory error", err)
				}
				if !strings.HasPrefix(err.Error(), test.wantPrefix) || strings.Contains(err.Error(), "required") {
					t.Fatalf("error = %q, want prefix %q and the real path-resolution failure", err, test.wantPrefix)
				}
			})
		}
		return
	}

	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit removing the process working directory")
	}
	directory := filepath.Join(t.TempDir(), "removed-working-directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAtomicOutputsPreserveAbsolutePathErrors$")
	command.Env = append(os.Environ(), removedWorkingDirectoryEnv+"="+directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("path-error subprocess failed: %v\n%s", err, output)
	}
}

func TestWriteOutputFileDoesNotReplaceDestinationOnFailure(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	destination := filepath.Join(directory, "records.csv")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("serialization failed")
	if err := writeOutputFile(destination, func(output io.Writer) error {
		if _, err := output.Write([]byte("partial")); err != nil {
			return err
		}
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("writeOutputFile() error = %v", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("destination = %q", data)
	}
}

func TestWriteOutputDirectoryPublishesAllOrNothing(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	destination := filepath.Join(directory, "artifacts")
	if err := writeOutputDirectory(destination, []namedOutput{
		{name: "create.csv", data: []byte("create")},
		{name: "agents.csv", data: []byte("agents")},
	}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"create.csv": "create", "agents.csv": "agents"} {
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
	if err := writeOutputDirectory(destination, []namedOutput{{name: "again.csv", data: []byte("again")}}); err == nil {
		t.Fatal("writeOutputDirectory(existing) error = nil")
	}
}

func TestWriteOutputDirectoryRejectsUnsafeNameBeforePublishing(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(t.TempDir(), "artifacts")
	err := writeOutputDirectory(destination, []namedOutput{{name: "../escape", data: []byte("bad")}})
	if err == nil {
		t.Fatal("writeOutputDirectory() error = nil")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists after failure: %v", err)
	}
}

func TestWriteNewFileConcurrentWritersPublishOneCompleteOutput(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(t.TempDir(), "review.csv")
	payloads := [][]byte{[]byte("first complete payload\n"), []byte("second complete payload\n")}
	start := make(chan struct{})
	writeErrors := make([]error, len(payloads))
	var wait sync.WaitGroup
	for index := range payloads {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			writeErrors[index] = writeNewFile(destination, payloads[index])
		}(index)
	}
	close(start)
	wait.Wait()
	successes := 0
	for _, writeErr := range writeErrors {
		if writeErr == nil {
			successes++
			continue
		}
		if !errors.Is(writeErr, os.ErrExist) {
			t.Fatalf("writeNewFile() error = %v", writeErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successful writers = %d, want 1", successes)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payloads[0]) && string(data) != string(payloads[1]) {
		t.Fatalf("destination contains partial bytes: %q", data)
	}
}
