// Package hosts owns host identity and local-agent bootstrap behavior.
package hosts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

type IdentityKind string

const (
	IdentityUUID     IdentityKind = "uuid"
	IdentityFallback IdentityKind = "fallback"
)

var ErrInvalidIdentity = errors.New("invalid filesystem identity")

// EncodeIdentityV1 encodes the immutable three-part filesystem identity.
func EncodeIdentityV1(kind IdentityKind, first, mountRoot, mountPoint string) ([]byte, error) {
	if kind != IdentityUUID && kind != IdentityFallback {
		return nil, fmt.Errorf("%w: unsupported kind", ErrInvalidIdentity)
	}
	fields := []string{first, mountRoot, mountPoint}
	encoded := []byte{0x01}
	for _, field := range fields {
		if !utf8.ValidString(field) || uint64(len(field)) > math.MaxUint32 {
			return nil, fmt.Errorf("%w: field is not encodable", ErrInvalidIdentity)
		}
		encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(field))) //nolint:gosec // bounds checked above (len <= math.MaxUint32)
		encoded = append(encoded, field...)
	}
	return encoded, nil
}
