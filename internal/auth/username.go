package auth

import (
	"fmt"
	"strings"
)

const maximumUsernameBytes = 64

// NormalizeUsername applies the canonical v1 administrator username grammar.
func NormalizeUsername(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 0 || len(trimmed) > maximumUsernameBytes {
		return "", fmt.Errorf("username must contain between 1 and 64 ASCII characters")
	}

	normalized := make([]byte, len(trimmed))
	for index := range len(trimmed) {
		character := trimmed[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}

		if index == 0 {
			if !isASCIILetterOrDigit(character) {
				return "", fmt.Errorf("username must start with an ASCII letter or digit")
			}
		} else if !isASCIILetterOrDigit(character) && character != '.' && character != '_' && character != '-' {
			return "", fmt.Errorf("username contains an unsupported character")
		}

		normalized[index] = character
	}

	return string(normalized), nil
}

func isASCIILetterOrDigit(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}
