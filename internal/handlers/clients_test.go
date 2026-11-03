package handlers

import "testing"

func TestNormalizeCNIC(t *testing.T) {
	ok := map[string]string{
		"3520212345671":     "35202-1234567-1",
		"35202-1234567-1":   "35202-1234567-1",
		" 35202 1234567 1 ": "35202-1234567-1",
	}
	for in, want := range ok {
		if got, good := normalizeCNIC(in); !good || got != want {
			t.Errorf("normalizeCNIC(%q) = %q, %v; want %q", in, got, good, want)
		}
	}
	for _, in := range []string{"35202-1234567", "352021234567123", "35202-12345A7-1", "abc"} {
		if _, good := normalizeCNIC(in); good {
			t.Errorf("normalizeCNIC(%q) accepted", in)
		}
	}
}
