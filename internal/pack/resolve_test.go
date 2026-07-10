package pack

import (
	"crypto/sha512"
	"encoding/hex"
	"testing"
)

func TestVerifyModrinthPack(t *testing.T) {
	t.Parallel()
	body := []byte("mrpack")
	sum := sha512.Sum512(body)
	if err := verifyModrinthPack(body, map[string]string{"sha512": hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
	if err := verifyModrinthPack([]byte("tampered"), map[string]string{"sha512": hex.EncodeToString(sum[:])}); err == nil {
		t.Fatal("verifyModrinthPack unexpectedly accepted tampered bytes")
	}
	if err := verifyModrinthPack(body, nil); err == nil {
		t.Fatal("verifyModrinthPack unexpectedly accepted missing checksums")
	}
}
