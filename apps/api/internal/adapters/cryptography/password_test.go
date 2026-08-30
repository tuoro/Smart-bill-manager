package cryptography

import "testing"

func TestPasswordHasherRoundTrip(t *testing.T) {
	t.Parallel()

	hasher, err := NewPasswordHasher(Argon2Params{
		MemoryKiB:   8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hasher.Hash([]byte("correct-horse-battery-staple"))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := hasher.Verify([]byte("correct-horse-battery-staple"), encoded)
	if err != nil || !valid {
		t.Fatalf("valid password = %v, error = %v", valid, err)
	}
	valid, err = hasher.Verify([]byte("wrong-password"), encoded)
	if err != nil || valid {
		t.Fatalf("wrong password = %v, error = %v", valid, err)
	}
}

func TestPasswordHasherRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := NewPasswordHasher(Argon2Params{}); err == nil {
		t.Fatal("unsafe parameters accepted")
	}
}
