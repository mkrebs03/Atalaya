package hosts

import (
	"encoding/hex"
	"testing"
)

func TestEncodeIdentityV1Golden(t *testing.T) {
	tests := []struct {
		kind   IdentityKind
		fields [3]string
		want   string
	}{
		{IdentityUUID, [3]string{"abcd-1234", "/", "/data"}, "0100000009616263642d31323334000000012f000000052f64617461"},
		{IdentityFallback, [3]string{"/dev/sda1", "/root", "/"}, "01000000092f6465762f73646131000000052f726f6f74000000012f"},
	}
	for _, tt := range tests {
		got, err := EncodeIdentityV1(tt.kind, tt.fields[0], tt.fields[1], tt.fields[2])
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(got) != tt.want {
			t.Fatalf("EncodeIdentityV1() = %x, want %s", got, tt.want)
		}
	}
}

func TestEncodeIdentityV1RejectsKindAndInvalidUTF8(t *testing.T) {
	if _, err := EncodeIdentityV1("other", "x", "/", "/"); err == nil {
		t.Fatal("unsupported kind accepted")
	}
	if _, err := EncodeIdentityV1(IdentityUUID, string([]byte{0xff}), "/", "/"); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
