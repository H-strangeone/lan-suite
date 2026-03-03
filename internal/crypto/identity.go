package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Identity struct {
	NodeID string

	PublicKey ed25519.PublicKey

	privateKey ed25519.PrivateKey
}

func LoadOrCreate(dataDir string) (*Identity, error) {
	privPath := filepath.Join(dataDir, "node.key")
	pubPath := filepath.Join(dataDir, "node.pub")

	if fileExists(privPath) && fileExists(pubPath) {
		return loadFromDisk(privPath, pubPath)
	}

	return generateAndSave(privPath, pubPath)
}

func (id *Identity) Sign(message []byte) []byte {

	return ed25519.Sign(id.privateKey, message)
}

func (id *Identity) PrivateKey() ed25519.PrivateKey {
	return id.privateKey
}

func Verify(publicKey ed25519.PublicKey, message, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

func generateAndSave(privPath, pubPath string) (*Identity, error) {

	if err := os.MkdirAll(filepath.Dir(privPath), 0700); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating ed25519 keypair: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return nil, fmt.Errorf("writing private key: %w", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling public key: %w", err)
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return nil, fmt.Errorf("writing public key: %w", err)
	}

	fmt.Printf("[identity] generated new keypair\n")
	fmt.Printf("[identity] private key: %s\n", privPath)
	fmt.Printf("[identity] public key:  %s\n", pubPath)

	return buildIdentity(pubKey, privKey), nil
}

func loadFromDisk(privPath, pubPath string) (*Identity, error) {

	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key: %w", err)
	}

	privBlock, _ := pem.Decode(privPEM)
	if privBlock == nil {
		return nil, errors.New("private key file is not valid PEM")
	}

	privKeyRaw, err := x509.ParsePKCS8PrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	privKey, ok := privKeyRaw.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("key file is not an Ed25519 private key")
	}

	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("reading public key: %w", err)
	}

	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil {
		return nil, errors.New("public key file is not valid PEM")
	}

	pubKeyRaw, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}

	pubKey, ok := pubKeyRaw.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("key file is not an Ed25519 public key")
	}

	return buildIdentity(pubKey, privKey), nil
}

func buildIdentity(pub ed25519.PublicKey, priv ed25519.PrivateKey) *Identity {

	hash := sha256.Sum256(pub)
	nodeID := hex.EncodeToString(hash[:])

	return &Identity{
		NodeID:     nodeID,
		PublicKey:  pub,
		privateKey: priv,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
