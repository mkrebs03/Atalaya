package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/mkrebs03/atalaya/internal/auth"
	"golang.org/x/term"
)

type passwordReader func(string, io.Writer) ([]byte, error)
type passwordHasher func([]byte) (string, error)

func readTerminalPassword(input *os.File, output io.Writer, prompt string) ([]byte, error) {
	if input == nil || output == nil {
		return nil, fmt.Errorf("password terminal is required")
	}
	fileDescriptor := int(input.Fd())
	if !term.IsTerminal(fileDescriptor) {
		return nil, fmt.Errorf("password input requires an interactive terminal")
	}
	if _, err := fmt.Fprint(output, prompt); err != nil {
		return nil, fmt.Errorf("write password prompt: %w", err)
	}
	password, err := readPasswordWithoutEcho(fileDescriptor)
	_, newlineErr := fmt.Fprintln(output)
	if err != nil {
		clear(password)
		return nil, fmt.Errorf("read password: %w", err)
	}
	if newlineErr != nil {
		clear(password)
		return nil, fmt.Errorf("write password prompt: %w", newlineErr)
	}
	return password, nil
}

func readConfirmedPasswordHash(
	reader passwordReader,
	hasher passwordHasher,
	output io.Writer,
) (string, error) {
	if reader == nil || hasher == nil || output == nil {
		return "", fmt.Errorf("password prompt dependencies are required")
	}

	password, err := reader("Password: ", output)
	if err != nil {
		clear(password)
		return "", err
	}
	defer clear(password)
	confirmation, err := reader("Confirm password: ", output)
	if err != nil {
		clear(confirmation)
		return "", err
	}
	defer clear(confirmation)

	if !bytes.Equal(password, confirmation) {
		return "", fmt.Errorf("password confirmation does not match")
	}
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := hasher(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}
