// Package totp is RFC 6238 SHA-1 six-digit codes with a ±1 step window.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const step = 30

func RandomSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func URI(secret, issuer, account string) string {
	secret = strings.ReplaceAll(strings.ToUpper(secret), " ", "")
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	label := url.PathEscape(issuer + ":" + account)
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func Code(secret string, t time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	counter := uint64(t.Unix() / step)
	return fmt.Sprintf("%06d", hotp(key, counter)), nil
}

func Verify(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	key, err := decodeSecret(secret)
	if err != nil {
		return false
	}
	now := time.Now().Unix() / step
	for _, d := range []int64{-1, 0, 1} {
		if fmt.Sprintf("%06d", hotp(key, uint64(now+d))) == code {
			return true
		}
	}
	return false
}

func decodeSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if n := len(s) % 8; n != 0 {
		s += strings.Repeat("=", 8-n)
	}
	return base32.StdEncoding.DecodeString(s)
}

func hotp(key []byte, counter uint64) uint32 {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return bin % 1000000
}

func ParseCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func MustCode(secret string, t time.Time) string {
	c, err := Code(secret, t)
	if err != nil {
		return ""
	}
	return c
}

func ValidSecret(secret string) bool {
	_, err := decodeSecret(secret)
	return err == nil && len(secret) >= 16
}

func Counter(t time.Time) uint64 { return uint64(t.Unix() / step) }

func FormatCode(n uint32) string { return strconv.FormatInt(int64(n), 10) }
