package secretbox

import (
	"strings"
	"testing"
)

const keyA = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
const keyB = "ff0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestRoundTrip(t *testing.T) {
	box, err := New(keyA)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "refresh-token") {
		t.Error("sealed value contains the plaintext")
	}
	again, _ := box.Seal("refresh-token")
	if again == sealed {
		t.Error("nonce was reused")
	}
	if got, err := box.Open(sealed); err != nil || got != "refresh-token" {
		t.Errorf("Open = %q, %v", got, err)
	}
}

func TestRejectsWrongKeyAndTampering(t *testing.T) {
	a, _ := New(keyA)
	b, _ := New(keyB)
	sealed, _ := a.Seal("secret")
	if _, err := b.Open(sealed); err == nil {
		t.Error("wrong key should fail")
	}
	if _, err := a.Open(sealed[:len(sealed)-4] + "AAAA"); err == nil {
		t.Error("altered data should fail")
	}
	if _, err := a.Open("not base64!"); err == nil {
		t.Error("malformed data should fail")
	}
}

func TestRejectsBadKeys(t *testing.T) {
	for _, k := range []string{"", "abcd", strings.Repeat("z", 64)} {
		if _, err := New(k); err == nil {
			t.Errorf("New(%q) should fail", k)
		}
	}
}
