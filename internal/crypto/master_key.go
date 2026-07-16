// Package crypto protects reusable Atalaya secrets and loads the server master key.
package crypto

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const masterKeySize = 32

// LoadMasterKey opens and validates a raw 32-byte master key without following links.
func LoadMasterKey(path string) ([]byte, error) {
	return loadMasterKey(path, nil)
}

func loadMasterKey(path string, afterOpen func()) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open master key: %w", err)
	}

	file := os.NewFile(uintptr(fd), "master-key")
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open master key: invalid file descriptor")
	}
	defer file.Close()

	if afterOpen != nil {
		afterOpen()
	}

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, fmt.Errorf("inspect master key: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("master key must be a regular file")
	}
	if stat.Mode&0o7777 != 0o600 {
		return nil, fmt.Errorf("master key file mode must be exactly 0600")
	}
	if stat.Size != masterKeySize {
		return nil, fmt.Errorf("master key must contain exactly 32 bytes")
	}

	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(file, key); err != nil {
		clear(key)
		return nil, fmt.Errorf("read master key: %w", err)
	}

	var extra [1]byte
	if _, err := file.Read(extra[:]); err != io.EOF {
		clear(key)
		if err != nil {
			return nil, fmt.Errorf("read master key: %w", err)
		}
		return nil, fmt.Errorf("master key must contain exactly 32 bytes")
	}

	return key, nil
}
