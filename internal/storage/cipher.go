package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	hkdfInfo = "lan-suite:chat-storage:v1"

	nonceSize = 12
)

type StorageCipher struct {
	aead cipher.AEAD
}

func NewStorageCipher(privKeySeed []byte) (*StorageCipher, error) {
	if len(privKeySeed) < 32 {
		return nil, errors.New("privKeySeed must be at least 32 bytes")
	}

	h := hkdf.New(sha256.New, privKeySeed[:32], nil, []byte(hkdfInfo))

	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(h, aesKey); err != nil {
		return nil, fmt.Errorf("deriving storage key: %w", err)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	for i := range aesKey {
		aesKey[i] = 0
	}

	return &StorageCipher{aead: aead}, nil
}

func (c *StorageCipher) Encrypt(plaintext []byte) ([]byte, error) {

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

func (c *StorageCipher) Decrypt(data []byte) ([]byte, error) {
	if len(data) < nonceSize+c.aead.Overhead() {
		return nil, errors.New("ciphertext too short — file may be corrupt or not encrypted")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {

		return nil, fmt.Errorf("decryption failed (wrong key or corrupted file): %w", err)
	}

	return plaintext, nil
}
