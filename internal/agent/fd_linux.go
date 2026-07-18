//go:build linux

package agent

import "os"

func newFDFile(fd int, name string) *os.File { return os.NewFile(uintptr(fd), name) }
