package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLoadMasterKeyReadsValidRawKey(t *testing.T) {
	t.Parallel()

	want := bytes.Repeat([]byte{0x5a}, masterKeySize)
	path := writeKeyFile(t, want, 0o600)

	got, err := LoadMasterKey(path)
	if err != nil {
		t.Fatalf("LoadMasterKey() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("LoadMasterKey() = %x, want %x", got, want)
	}
}

func TestLoadMasterKeyRejectsWrongSizeModeAndType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*testing.T) string
		match string
	}{
		{name: "short", setup: func(t *testing.T) string { return writeKeyFile(t, make([]byte, 31), 0o600) }, match: "exactly 32"},
		{name: "long", setup: func(t *testing.T) string { return writeKeyFile(t, make([]byte, 33), 0o600) }, match: "exactly 32"},
		{name: "group readable", setup: func(t *testing.T) string { return writeKeyFile(t, make([]byte, 32), 0o640) }, match: "exactly 0600"},
		{name: "executable", setup: func(t *testing.T) string { return writeKeyFile(t, make([]byte, 32), 0o700) }, match: "exactly 0600"},
		{name: "directory", setup: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "key-directory")
			if err := os.Mkdir(path, 0o600); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			return path
		}, match: "regular file"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadMasterKey(test.setup(t))
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("LoadMasterKey() error = %v, want containing %q", err, test.match)
			}
		})
	}
}

func TestLoadMasterKeyRejectsSymlink(t *testing.T) {
	t.Parallel()

	target := writeKeyFile(t, make([]byte, masterKeySize), 0o600)
	link := filepath.Join(t.TempDir(), "master.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	if _, err := LoadMasterKey(link); err == nil {
		t.Fatal("LoadMasterKey(symlink) error = nil")
	}
}

func TestLoadMasterKeyRejectsFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "master.key")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo() error = %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := LoadMasterKey(path)
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("LoadMasterKey(FIFO) error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("LoadMasterKey(FIFO) blocked")
	}
}

func TestLoadMasterKeyUsesOpenedDescriptorDuringReplacement(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "master.key")
	original := bytes.Repeat([]byte{0x11}, masterKeySize)
	replacement := bytes.Repeat([]byte{0x22}, masterKeySize)
	writeFile(t, path, original, 0o600)

	got, err := loadMasterKey(path, func() {
		replacementPath := filepath.Join(directory, "replacement.key")
		writeFile(t, replacementPath, replacement, 0o600)
		if err := os.Rename(replacementPath, path); err != nil {
			t.Fatalf("Rename() error = %v", err)
		}
	})
	if err != nil {
		t.Fatalf("LoadMasterKey() error = %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("LoadMasterKey() = %x, want opened file %x", got, original)
	}
}

func writeKeyFile(t *testing.T, content []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	writeFile(t, path, content, mode)
	return path
}

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
}
