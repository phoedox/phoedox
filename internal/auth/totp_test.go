package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPVectors(t *testing.T) {
	// RFC 6238 appendix B (SHA1), last six digits.
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(secret, unix/30); got != want {
			t.Errorf("T=%d: got %s want %s", unix, got, want)
		}
	}
}

func TestTOTPVerify(t *testing.T) {
	secret := newTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	cur := now.Unix() / 30
	code := totpCode(secret, cur)
	step, ok := totpVerify(secret, code[:3]+" "+code[3:], now, 0)
	if !ok || step != cur {
		t.Fatal("current code with space rejected")
	}
	if _, ok := totpVerify(secret, code, now, step); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok := totpVerify(secret, totpCode(secret, cur-1), now, 0); !ok {
		t.Fatal("previous step rejected")
	}
	if _, ok := totpVerify(secret, totpCode(secret, cur-2), now, 0); ok {
		t.Fatal("old code accepted")
	}
	if _, ok := totpVerify(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
	uri := totpURI(secret, "Phoedox", "jan@example.org")
	if !strings.HasPrefix(uri, "otpauth://totp/Phoedox:jan@example.org?") || !strings.Contains(uri, "secret="+b32.EncodeToString(secret)) {
		t.Fatalf("uri: %s", uri)
	}
	svg, err := qrSVG(uri)
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "M4 4h1v1h-1z") {
		t.Fatalf("qr: %v %.80s", err, svg)
	}
}
