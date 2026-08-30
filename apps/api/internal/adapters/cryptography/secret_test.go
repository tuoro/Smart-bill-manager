package cryptography

import (
	"bytes"
	"testing"
)

func TestSecretCipherRoundTripAndAuthentication(t *testing.T) {
	t.Parallel()

	cipher, err := NewSecretCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("synthetic-api-key")
	encrypted, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypt = %q, %v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := cipher.Decrypt(encrypted); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestSecretFingerprintIsStableAndLengthDelimited(t *testing.T) {
	t.Parallel()

	cipher, _ := NewSecretCipher(bytes.Repeat([]byte{0x24}, 32))
	first := cipher.Fingerprint([]byte("ab"), []byte("c"))
	second := cipher.Fingerprint([]byte("a"), []byte("bc"))
	if first == second {
		t.Fatal("fingerprint omitted part boundaries")
	}
	if first != cipher.Fingerprint([]byte("ab"), []byte("c")) {
		t.Fatal("fingerprint is not stable")
	}
}
