package api

import "crypto/rand"

// readCryptoRand reads cryptographically secure random bytes.
// This wrapper exists to keep the import in one place.
func readCryptoRand(b []byte) (int, error) {
	return rand.Read(b)
}