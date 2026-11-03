package auth

import (
	"testing"
	"time"
)

// RFC 6238 test vector (SHA-1): secret "12345678901234567890", T=59 → 94287082 (8 digits) → 287082.
func TestTOTPVector(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	step, ok := CheckTOTP(secret, "287082", 0, time.Unix(59, 0))
	if !ok || step != 1 {
		t.Fatalf("RFC vector failed: %v %v", step, ok)
	}
	if _, ok := CheckTOTP(secret, "287082", 1, time.Unix(59, 0)); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok := CheckTOTP(secret, "000000", 0, time.Unix(59, 0)); ok {
		t.Fatal("wrong code accepted")
	}
	// 1111111109 → 07081804 → 081804
	if _, ok := CheckTOTP(secret, "081804", 0, time.Unix(1111111109, 0)); !ok {
		t.Fatal("second RFC vector failed")
	}
}

func TestPasswordPolicy(t *testing.T) {
	bad := map[string]string{"short1": "", "abcdefghij": "", "Password2026": "", "aaaa1111aa": "",
		"ahmed2026xyz": "ahmed@x.pk", "Bismillah786": ""}
	for pw, email := range bad {
		if PasswordProblem(pw, email, "") == "" {
			t.Errorf("%q should be refused", pw)
		}
	}
	if m := PasswordProblem("Tiger-Lamp-49-Kite", "ahmed@x.pk", "Ahmed Khan"); m != "" {
		t.Errorf("good password refused: %s", m)
	}
}

func TestSeal(t *testing.T) {
	appKey = make([]byte, 32)
	s, err := Seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open(s)
	if err != nil || p != "JBSWY3DPEHPK3PXP" {
		t.Fatal("seal/open round trip failed")
	}
	appKey[0] = 1
	if _, err := Open(s); err == nil {
		t.Fatal("opened with the wrong key")
	}
}
