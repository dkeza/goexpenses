package util

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func TestNewPublicIDUsesExistingHexadecimalFormat(t *testing.T) {
	first, err := NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}
	second, err := NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}

	if first == second {
		t.Fatal("NewPublicID returned the same identifier twice")
	}
	for _, publicID := range []string{first, second} {
		decoded, err := hex.DecodeString(publicID)
		if err != nil || len(decoded) != publicIDByteSize {
			t.Fatalf("public ID %q is not %d bytes of hexadecimal data", publicID, publicIDByteSize)
		}
	}
}

func TestNewPublicIDReadsAllRandomBytes(t *testing.T) {
	randomBytes := bytes.Repeat([]byte{0xab}, publicIDByteSize)
	publicID, err := newPublicID(bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("newPublicID: %v", err)
	}
	if want := hex.EncodeToString(randomBytes); publicID != want {
		t.Fatalf("newPublicID = %q, want %q", publicID, want)
	}
}

func TestNewPublicIDReturnsRandomSourceError(t *testing.T) {
	expectedError := errors.New("random source failed")
	_, err := newPublicID(errorReader{err: expectedError})
	if !errors.Is(err, expectedError) {
		t.Fatalf("newPublicID error = %v, want wrapped random source error", err)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

var _ io.Reader = errorReader{}
