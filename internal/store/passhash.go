package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// passwordHashCost is the bcrypt cost for new password hashes. Legacy
// SHA-256(salt:password) hashes remain verifiable for snapshot migration.
const passwordHashCost = bcrypt.DefaultCost

// hashPassword returns a random 16-byte salt (used as a non-secret session
// credential / rotation token) and a bcrypt hash of the password. The salt is
// independent of bcrypt's embedded salt so PasswordCredential can detect
// rotate/clear without exposing the password material.
//
// Plaintext secrets never land on disk; only salt+hash are stored.
func hashPassword(password string) (saltHex, hashHex string, err error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", "", err
	}
	saltHex = hex.EncodeToString(salt[:])
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordHashCost)
	if err != nil {
		return "", "", err
	}
	return saltHex, string(hash), nil
}

// verifyPassword checks a presented password against a stored salt+hash in
// constant time where practical. Empty hash/password never matches. The
// presented password is trimmed of surrounding whitespace so verification
// matches the set path, which always stores TrimSpace'd values.
//
// Supports:
//   - modern bcrypt hashes (prefix "$2")
//   - legacy SHA-256(saltHex + ":" + password) hex digests from older snapshots
func verifyPassword(saltHex, hashHex, password string) bool {
	password = strings.TrimSpace(password)
	if hashHex == "" || password == "" {
		return false
	}
	if strings.HasPrefix(hashHex, "$2") {
		// bcrypt embeds its own salt; PasswordSalt is only a credential token.
		return bcrypt.CompareHashAndPassword([]byte(hashHex), []byte(password)) == nil
	}
	// Legacy fast hash — still accepted so on-disk snapshots keep working.
	if saltHex == "" {
		return false
	}
	sum := sha256.Sum256([]byte(saltHex + ":" + password))
	want := hex.EncodeToString(sum[:])
	if len(want) != len(hashHex) {
		_ = subtle.ConstantTimeCompare([]byte(want), []byte(want))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(hashHex)) == 1
}
