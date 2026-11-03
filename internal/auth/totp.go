package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Two-step sign-in with an authenticator app (Google / Microsoft
// Authenticator, Authy…): TOTP, RFC 6238 — 6 digits, 30-second steps,
// SHA-1, one step of clock drift allowed either way. A code is accepted
// only once (the last used step is remembered).

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret: 160 random bits, base32 (what the app is given).
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

func totpAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", v)
}

// CheckTOTP: is code valid now for the secret, at a step after lastStep?
// Returns the step it matched (to store as the new lastStep).
func CheckTOTP(secretB32, code string, lastStep int64, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	key, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		return 0, false
	}
	cur := now.Unix() / 30
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpAt(key, s)), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// TOTPURI: what the QR code holds (otpauth://totp/Issuer:account?...).
func TOTPURI(secretB32, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secretB32)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// NewRecoveryCodes: n one-time codes like "k7m2-9xq4" (shown once), and
// their hashes (stored).
func NewRecoveryCodes(n int) (codes []string, hashes []string) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	for i := 0; i < n; i++ {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		var sb strings.Builder
		for j, x := range b {
			if j == 4 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(x)%len(alphabet)])
		}
		codes = append(codes, sb.String())
		hashes = append(hashes, KeyedHash("recovery:"+sb.String()))
	}
	return
}

// NormalizeRecovery: "K7M2 9XQ4" → "k7m2-9xq4".
func NormalizeRecovery(s string) string {
	s = strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(s)))
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}
