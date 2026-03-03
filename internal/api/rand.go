package api

import "crypto/rand"

func readCryptoRand(b []byte) (int, error) {
	return rand.Read(b)
}
