package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const (
	BcryptCost = 12
)

func HashSecret(secret string) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("secret cannot be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing secret: %w", err)
	}

	return string(hash), nil
}

func VerifySecret(hash, secret string) error {

	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret))
	if err != nil {

		return fmt.Errorf("invalid secret")
	}
	return nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()

	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
