package payloadcrypto

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/logoscore/logos-golang-protocol/protocol"
)

func TestSupported(t *testing.T) {
	if !Supported(protocol.CryptoAES256CBCHMACSHA256) {
		t.Fatal("aes-256-cbc-hmac-sha256 not supported")
	}
	if Supported("x25519-xchacha20poly1305") || Supported("") {
		t.Fatal("unexpected suite supported")
	}
}

func TestNewKeys(t *testing.T) {
	a, err := NewKeys(protocol.CryptoAES256CBCHMACSHA256)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewKeys(protocol.CryptoAES256CBCHMACSHA256)
	var ma, mb protocol.AES256CBCHMACSHA256Material
	if err := json.Unmarshal(a.Material, &ma); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b.Material, &mb); err != nil {
		t.Fatal(err)
	}
	if len(ma.EncKey) != 32 || len(ma.MACKey) != 32 {
		t.Fatalf("key sizes %d, %d", len(ma.EncKey), len(ma.MACKey))
	}
	if bytes.Equal(ma.EncKey, ma.MACKey) || bytes.Equal(ma.EncKey, mb.EncKey) {
		t.Fatal("keys repeat")
	}
	if _, err := NewKeys("rot13"); err == nil {
		t.Fatal("rot13 keys")
	}
}
