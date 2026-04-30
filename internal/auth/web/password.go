package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters tuned for an interactive login (~50ms on a server core).
const (
	a2Time    = 2
	a2Memory  = 64 * 1024
	a2Threads = 1
	a2KeyLen  = 32
	a2SaltLen = 16
)

// HashPassword returns a string of the form "argon2id$<saltB64>$<hashB64>".
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, a2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt, a2Time, a2Memory, a2Threads, a2KeyLen)
	return fmt.Sprintf("argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword constant-time compares a plain password against a stored hash.
func VerifyPassword(plain, stored string) (bool, error) {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false, errors.New("unrecognised hash format")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, a2Time, a2Memory, a2Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
