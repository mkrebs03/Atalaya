//go:build !linux

package agent

import "os"

// The descriptor returned by the supported Unix implementations can be
// wrapped by os.File on non-Linux development hosts as well. Linux keeps its
// own file because the collector has Linux-only build constraints.
func newFDFile(fd int, name string) *os.File { return os.NewFile(uintptr(fd), name) }
