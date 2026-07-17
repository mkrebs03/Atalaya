//go:build !linux

package cli

import "golang.org/x/term"

func readPasswordWithoutEcho(fileDescriptor int) ([]byte, error) {
	return term.ReadPassword(fileDescriptor)
}
