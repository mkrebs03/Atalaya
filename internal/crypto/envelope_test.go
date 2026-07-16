package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestEnvelopeV1GoldenVector(t *testing.T) {
	t.Parallel()

	key := sequentialBytes(32)
	nonce := sequentialBytes(envelopeNonceSize)
	envelope, err := seal(key, SentinelSecretType, SentinelEntityID, []byte(SentinelPlaintext), bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("seal() error = %v", err)
	}

	const expectedEnvelopeHex = "01000102030405060708090a0b2676b777a49ca336e020e4ffd49b5506e6afaa57981e3c171511d44517b1275739783173c5f72372e2752a"
	if got := hex.EncodeToString(envelope); got != expectedEnvelopeHex {
		t.Fatalf("envelope = %s, want %s", got, expectedEnvelopeHex)
	}

	const expectedAADHex = "6174616c61796100736563726574007631000008696e7374616e636500106d61737465722d6b65792d636865636b"
	additionalData, err := buildAdditionalData(SentinelSecretType, SentinelEntityID)
	if err != nil {
		t.Fatalf("buildAdditionalData() error = %v", err)
	}
	if got := hex.EncodeToString(additionalData); got != expectedAADHex {
		t.Fatalf("AAD = %s, want %s", got, expectedAADHex)
	}
}

func TestOpenDecryptsPersistedGoldenVector(t *testing.T) {
	t.Parallel()

	const persistedEnvelopeHex = "01000102030405060708090a0b2676b777a49ca336e020e4ffd49b5506e6afaa57981e3c171511d44517b1275739783173c5f72372e2752a"
	envelope, err := hex.DecodeString(persistedEnvelopeHex)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}

	plaintext, err := Open(sequentialBytes(32), SentinelSecretType, SentinelEntityID, envelope)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(plaintext) != SentinelPlaintext {
		t.Fatalf("Open() = %q, want sentinel", plaintext)
	}
}

func TestSealAndOpenRoundTrip(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x7c}, masterKeySize)
	plaintext := []byte("discord-webhook-secret")
	envelope, err := Seal(key, "notification-channel", "channel-123", plaintext)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(envelope, plaintext) {
		t.Fatal("Seal() envelope contains plaintext")
	}

	opened, err := Open(key, "notification-channel", "channel-123", envelope)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open() = %q, want %q", opened, plaintext)
	}
}

func TestSealUsesDistinctNonces(t *testing.T) {
	t.Parallel()

	key := make([]byte, masterKeySize)
	first, err := Seal(key, "agent", "one", []byte("secret"))
	if err != nil {
		t.Fatalf("Seal(first) error = %v", err)
	}
	second, err := Seal(key, "agent", "one", []byte("secret"))
	if err != nil {
		t.Fatalf("Seal(second) error = %v", err)
	}
	if bytes.Equal(first[1:1+envelopeNonceSize], second[1:1+envelopeNonceSize]) {
		t.Fatal("Seal() reused a nonce")
	}
}

func TestOpenRejectsTamperingWrongContextAndUnknownVersion(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x35}, masterKeySize)
	envelope, err := seal(key, "agent", "host-1", []byte("credential"), bytes.NewReader(make([]byte, envelopeNonceSize)))
	if err != nil {
		t.Fatalf("seal() error = %v", err)
	}

	tampered := append([]byte(nil), envelope...)
	tampered[len(tampered)-1] ^= 0x80
	unknownVersion := append([]byte(nil), envelope...)
	unknownVersion[0] = 0x02
	tests := []struct {
		name       string
		secretType string
		entityID   string
		envelope   []byte
	}{
		{name: "tampered", secretType: "agent", entityID: "host-1", envelope: tampered},
		{name: "wrong type", secretType: "notification", entityID: "host-1", envelope: envelope},
		{name: "wrong ID", secretType: "agent", entityID: "host-2", envelope: envelope},
		{name: "unknown version", secretType: "agent", entityID: "host-1", envelope: unknownVersion},
		{name: "truncated", secretType: "agent", entityID: "host-1", envelope: envelope[:28]},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plaintext, err := Open(key, test.secretType, test.entityID, test.envelope)
			if !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("Open() error = %v, want ErrInvalidEnvelope", err)
			}
			if plaintext != nil {
				t.Fatalf("Open() plaintext = %x, want nil", plaintext)
			}
		})
	}
}

func TestEnvelopeRejectsInvalidKeyAndContext(t *testing.T) {
	t.Parallel()

	if _, err := Seal(make([]byte, 31), "agent", "host", nil); err == nil {
		t.Fatal("Seal(short key) error = nil")
	}
	if _, err := Open(make([]byte, 33), "agent", "host", make([]byte, 29)); err == nil {
		t.Fatal("Open(long key) error = nil")
	}
	if _, err := Seal(make([]byte, 32), strings.Repeat("a", 65536), "host", nil); err == nil {
		t.Fatal("Seal(oversized type) error = nil")
	}
	if _, err := Seal(make([]byte, 32), "agent", string([]byte{0xff}), nil); err == nil {
		t.Fatal("Seal(invalid UTF-8 ID) error = nil")
	}
}

func TestSealReportsRandomSourceFailure(t *testing.T) {
	t.Parallel()

	_, err := seal(make([]byte, masterKeySize), "agent", "host", nil, bytes.NewReader(nil))
	if err == nil || !strings.Contains(err.Error(), "generate secret nonce") {
		t.Fatalf("seal() error = %v", err)
	}
}

func sequentialBytes(length int) []byte {
	result := make([]byte, length)
	for index := range result {
		result[index] = byte(index)
	}
	return result
}
