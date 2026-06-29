package cronometer

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: RFC 6238 TOTP is defined over HMAC-SHA1; this is not a security-sensitive hash use.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// TOTP parameters per RFC 6238 defaults, which is what Cronometer's authenticator-app 2FA uses.
const (
	totpPeriod  = 30
	totpDigits  = 6
	totpModulus = 1_000_000 // 10^totpDigits
)

// GenerateTOTP returns the RFC 6238 time-based one-time password for the given base32 secret at
// time t (6 digits, 30-second step, HMAC-SHA1).
func GenerateTOTP(secret string, t time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}

	counter := uint64(t.Unix()) / totpPeriod
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3).
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])

	return fmt.Sprintf("%0*d", totpDigits, code%totpModulus), nil
}

// decodeTOTPSecret normalizes and base32-decodes a TOTP secret as copied from an authenticator
// setup screen (spaces and case are ignored; padding optional).
func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	s = strings.TrimRight(s, "=")
	if s == "" {
		return nil, fmt.Errorf("empty TOTP secret")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decoding TOTP secret: %w", err)
	}
	return key, nil
}

// TOTPCode returns the current TOTP code when a secret is configured. The bool reports whether a
// secret was present; when false, the caller should prompt for a code interactively instead.
func (c *Config) TOTPCode(now time.Time) (code string, present bool, err error) {
	if c.TOTPSecret == "" {
		return "", false, nil
	}
	code, err = GenerateTOTP(c.TOTPSecret, now)
	if err != nil {
		return "", false, err
	}
	return code, true, nil
}
