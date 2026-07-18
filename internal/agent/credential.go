package agent

import (
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/sys/unix"
)

func LoadCredential(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open agent credential: %w", err)
	}
	file := newFDFile(fd, path)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, fmt.Errorf("inspect agent credential: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o777 != 0o600 {
		return nil, fmt.Errorf("agent credential must be a regular file with mode 0600")
	}
	b, err := io.ReadAll(io.LimitReader(file, 45))
	if err != nil {
		return nil, fmt.Errorf("read agent credential: %w", err)
	}
	if len(b) == 44 && b[43] == '\n' {
		b = b[:43]
	}
	if len(b) != 43 {
		return nil, fmt.Errorf("agent credential has invalid encoding")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(b))
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("agent credential has invalid encoding")
	}
	return decoded, nil
}
