package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestReadTerminalPasswordRejectsRedirectedInput(t *testing.T) {
	t.Parallel()

	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()

	if _, err := readTerminalPassword(input, &bytes.Buffer{}, "Password: "); err == nil {
		t.Fatal("readTerminalPassword(pipe) error = nil")
	}
}

func TestReadConfirmedPasswordHashPromptsHashesAndClearsBuffers(t *testing.T) {
	t.Parallel()

	password := []byte("correct horse battery staple")
	confirmation := append([]byte(nil), password...)
	prompts := make([]string, 0, 2)
	reads := 0
	reader := func(prompt string, _ io.Writer) ([]byte, error) {
		prompts = append(prompts, prompt)
		reads++
		if reads == 1 {
			return password, nil
		}
		return confirmation, nil
	}
	hasher := func(value []byte) (string, error) {
		if string(value) != "correct horse battery staple" {
			t.Fatalf("hasher input = %q", value)
		}
		return "strict-phc", nil
	}

	hash, err := readConfirmedPasswordHash(reader, hasher, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("readConfirmedPasswordHash() error = %v", err)
	}
	if hash != "strict-phc" {
		t.Fatalf("hash = %q", hash)
	}
	if !reflect.DeepEqual(prompts, []string{"Password: ", "Confirm password: "}) {
		t.Fatalf("prompts = %#v", prompts)
	}
	if !allZero(password) || !allZero(confirmation) {
		t.Fatal("password buffers were not cleared")
	}
}

func TestReadConfirmedPasswordHashRejectsMismatchAndInvalidPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values [][]byte
	}{
		{name: "mismatch", values: [][]byte{[]byte("correct horse battery staple"), []byte("different horse battery staple")}},
		{name: "too short", values: [][]byte{[]byte("short"), []byte("short")}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			index := 0
			reader := func(string, io.Writer) ([]byte, error) {
				value := test.values[index]
				index++
				return value, nil
			}
			hasher := func([]byte) (string, error) {
				t.Fatal("hasher called for rejected password")
				return "", nil
			}
			if _, err := readConfirmedPasswordHash(reader, hasher, &bytes.Buffer{}); err == nil {
				t.Fatal("readConfirmedPasswordHash() error = nil")
			}
			for _, value := range test.values {
				if !allZero(value) {
					t.Fatal("password buffer was not cleared")
				}
			}
		})
	}
}

func TestReadConfirmedPasswordHashClearsFirstBufferWhenConfirmationFails(t *testing.T) {
	t.Parallel()

	password := []byte("correct horse battery staple")
	confirmation := []byte("partial confirmation")
	reads := 0
	reader := func(string, io.Writer) ([]byte, error) {
		reads++
		if reads == 1 {
			return password, nil
		}
		return confirmation, errors.New("terminal failed")
	}
	if _, err := readConfirmedPasswordHash(reader, func([]byte) (string, error) {
		return "", nil
	}, &bytes.Buffer{}); err == nil {
		t.Fatal("readConfirmedPasswordHash() error = nil")
	}
	if !allZero(password) || !allZero(confirmation) {
		t.Fatal("password buffers were not cleared")
	}
}

func TestReadConfirmedPasswordHashClearsPartialFirstRead(t *testing.T) {
	t.Parallel()

	password := []byte("partial password")
	reader := func(string, io.Writer) ([]byte, error) {
		return password, errors.New("terminal failed")
	}
	if _, err := readConfirmedPasswordHash(reader, func([]byte) (string, error) {
		return "", nil
	}, &bytes.Buffer{}); err == nil {
		t.Fatal("readConfirmedPasswordHash() error = nil")
	}
	if !allZero(password) {
		t.Fatal("partial password buffer was not cleared")
	}
}

func allZero(value []byte) bool {
	for _, current := range value {
		if current != 0 {
			return false
		}
	}
	return true
}
