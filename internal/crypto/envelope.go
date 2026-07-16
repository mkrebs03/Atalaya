package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

const (
	// EnvelopeVersionV1 identifies the persisted AES-256-GCM envelope format.
	EnvelopeVersionV1 byte = 0x01

	SentinelSecretType = "instance"
	SentinelEntityID   = "master-key-check"
	SentinelPlaintext  = "atalaya-master-key-check-v1"

	envelopeNonceSize = 12
	aadPrefix         = "atalaya\x00secret\x00v1\x00"
)

// ErrInvalidEnvelope reports that encrypted data could not be safely authenticated.
var ErrInvalidEnvelope = errors.New("invalid encrypted secret")

// Seal encrypts plaintext using the persisted v1 envelope and entity-bound AAD.
func Seal(key []byte, secretType, entityID string, plaintext []byte) ([]byte, error) {
	return seal(key, secretType, entityID, plaintext, cryptorand.Reader)
}

func seal(key []byte, secretType, entityID string, plaintext []byte, random io.Reader) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	additionalData, err := buildAdditionalData(secretType, entityID)
	if err != nil {
		return nil, err
	}

	envelope := make([]byte, 1+envelopeNonceSize, 1+envelopeNonceSize+len(plaintext)+gcm.Overhead())
	envelope[0] = EnvelopeVersionV1
	nonce := envelope[1 : 1+envelopeNonceSize]
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, fmt.Errorf("generate secret nonce: %w", err)
	}

	return gcm.Seal(envelope, nonce, plaintext, additionalData), nil
}

// Open authenticates and decrypts a persisted v1 envelope for the same entity context.
func Open(key []byte, secretType, entityID string, envelope []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	if len(envelope) < 1+envelopeNonceSize+gcm.Overhead() {
		return nil, ErrInvalidEnvelope
	}
	if envelope[0] != EnvelopeVersionV1 {
		return nil, ErrInvalidEnvelope
	}

	additionalData, err := buildAdditionalData(secretType, entityID)
	if err != nil {
		return nil, err
	}

	nonce := envelope[1 : 1+envelopeNonceSize]
	plaintext, err := gcm.Open(nil, nonce, envelope[1+envelopeNonceSize:], additionalData)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("master key must contain exactly 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize secret cipher: %w", err)
	}
	if gcm.NonceSize() != envelopeNonceSize {
		return nil, fmt.Errorf("initialize secret cipher: unexpected nonce size")
	}
	return gcm, nil
}

func buildAdditionalData(secretType, entityID string) ([]byte, error) {
	if !utf8.ValidString(secretType) || !utf8.ValidString(entityID) {
		return nil, fmt.Errorf("secret type and entity ID must be valid UTF-8")
	}
	if len(secretType) > math.MaxUint16 || len(entityID) > math.MaxUint16 {
		return nil, fmt.Errorf("secret type and entity ID must not exceed 65535 bytes")
	}

	additionalData := make([]byte, 0, len(aadPrefix)+4+len(secretType)+len(entityID))
	additionalData = append(additionalData, aadPrefix...)
	additionalData = binary.BigEndian.AppendUint16(additionalData, uint16(len(secretType)))
	additionalData = append(additionalData, secretType...)
	additionalData = binary.BigEndian.AppendUint16(additionalData, uint16(len(entityID)))
	additionalData = append(additionalData, entityID...)
	return additionalData, nil
}
