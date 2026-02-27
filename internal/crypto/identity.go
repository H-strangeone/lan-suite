// Package crypto handles all cryptographic operations for the LAN Suite node:
// key generation, signing, verification, and hashing.
// This package has NO external dependencies — only Go standard library.
package crypto

/*
  CONCEPT: Why Ed25519?
  ──────────────────────
  Ed25519 is an elliptic curve signature scheme.
  In a keypair:
  - Private key: secret. Used to SIGN messages. Never shared.
  - Public key:  shared with everyone. Used to VERIFY signatures.

  Properties:
  - Fast: signing/verifying is ~100x faster than RSA-2048
  - Small keys: 32 bytes private, 32 bytes public (RSA is 256+ bytes)
  - Secure: no known practical attacks, even quantum-resistant preparations
  - Deterministic: same message + same key = same signature (no random needed)

  WHY DOES A LAN NODE NEED A KEYPAIR?
  ─────────────────────────────────────
  Without cryptographic identity, any node could claim to be any other node.
  With Ed25519:
  1. Node generates keypair ONCE, saves to disk
  2. Node ID = SHA256(publicKey) → a stable 64-char hex string
  3. When connecting to a peer, node signs a challenge with its private key
  4. Peer verifies signature with the claimed public key
  5. If valid: "yes, you really are who you claim to be"
  This is exactly how SSH host key authentication works.

  CONCEPT: Encoding — PEM format
  ───────────────────────────────
  PEM (Privacy Enhanced Mail) is a base64-encoded format with
  -----BEGIN SOMETHING----- / -----END SOMETHING----- markers.
  It's the universal format for storing keys, certificates, etc.
  You'll recognize it from SSH keys, SSL certificates, etc.
*/

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

// Identity holds a node's keypair and derived ID.
// The NodeID is stable — it's deterministically derived from the public key.
type Identity struct {
	// NodeID is the hex-encoded SHA256 of the public key.
	// This is what peers see and refer to this node by.
	// Format: "a3f2b1..." (64 hex characters = 32 bytes)
	NodeID string

	// PublicKey is shared with peers for signature verification.
	PublicKey ed25519.PublicKey

	// privateKey is unexported — it never leaves this package unintentionally.
	// Only Sign() can use it.
	privateKey ed25519.PrivateKey
}

/*
  CONCEPT: Error wrapping with fmt.Errorf and %w
  ─────────────────────────────────────────────────
  Go doesn't have exceptions. Errors are return values.
  Convention: functions that can fail return (result, error).
  Caller checks: if err != nil { handle it }

  fmt.Errorf("loading key: %w", err) WRAPS the original error.
  The %w verb (not %v) makes the wrapped error inspectable.
  Callers can use errors.Is(err, specificError) to check the chain.

  This creates a chain:
  "loading identity: loading key: open ./data/node.key: no such file or directory"
  Each layer adds context about WHERE the error happened.
*/

// LoadOrCreate loads the node identity from disk.
// If no identity exists at dataDir, it generates a new keypair and saves it.
// This means: first boot = generates keys. Every subsequent boot = loads same keys.
func LoadOrCreate(dataDir string) (*Identity, error) {
	privPath := filepath.Join(dataDir, "node.key")
	pubPath  := filepath.Join(dataDir, "node.pub")

	// Check if keypair already exists
	if fileExists(privPath) && fileExists(pubPath) {
		return loadFromDisk(privPath, pubPath)
	}

	// First boot — generate new keypair
	return generateAndSave(privPath, pubPath)
}

// Sign signs a message with this node's private key.
// Returns a 64-byte Ed25519 signature.
// The peer can verify this with our PublicKey.
func (id *Identity) Sign(message []byte) []byte {
	/*
	  CONCEPT: Digital signatures
	  ─────────────────────────────
	  Sign(privateKey, message) → signature
	  Verify(publicKey, message, signature) → true/false

	  What we sign in practice:
	  - A challenge string the peer sends us (proves we have the private key)
	  - Handshake messages (proves message wasn't tampered with)
	  - Important CCN data packets (proves data came from claimed source)

	  NEVER sign: user passwords, raw data you didn't generate
	*/
	return ed25519.Sign(id.privateKey, message)
}

// Verify checks a signature against a message using a public key.
// Use this to verify a message claimed to be from another peer.
func Verify(publicKey ed25519.PublicKey, message, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

// ── Private helpers ──────────────────────────────────────────────────────────

func generateAndSave(privPath, pubPath string) (*Identity, error) {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(privPath), 0700); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	/*
	  crypto/rand.Reader is the OS cryptographic random number generator.
	  On Linux: reads from /dev/urandom
	  On Windows: uses CryptGenRandom
	  NEVER use math/rand for cryptographic purposes — it's predictable.
	*/
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating ed25519 keypair: %w", err)
	}

	// Save private key as PEM
	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	/*
	  File permissions: 0600 = owner read+write only.
	  The private key must NEVER be readable by other users.
	  0600 is the standard for SSH private keys too (ssh-add enforces this).
	  On Windows this is advisory, on Unix it's enforced by the kernel.
	*/
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return nil, fmt.Errorf("writing private key: %w", err)
	}

	// Save public key as PEM
	pubBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling public key: %w", err)
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	// Public key: 0644 = owner read+write, group+others read-only
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return nil, fmt.Errorf("writing public key: %w", err)
	}

	fmt.Printf("[identity] generated new keypair\n")
	fmt.Printf("[identity] private key: %s\n", privPath)
	fmt.Printf("[identity] public key:  %s\n", pubPath)

	return buildIdentity(pubKey, privKey), nil
}

func loadFromDisk(privPath, pubPath string) (*Identity, error) {
	// Load and decode private key
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

	// Load and decode public key
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

// buildIdentity constructs an Identity and derives the NodeID.
func buildIdentity(pub ed25519.PublicKey, priv ed25519.PrivateKey) *Identity {
	/*
	  CONCEPT: Deriving NodeID from PublicKey
	  ──────────────────────────────────────────
	  NodeID = hex(SHA256(publicKey))

	  Why SHA256 and not just the public key directly?
	  1. Fixed length: SHA256 always produces 32 bytes → 64 hex chars
	     Ed25519 public key is also 32 bytes, but this pattern
	     generalizes to other key types too
	  2. Hides key structure: the ID looks the same regardless of key algorithm
	  3. Fingerprint: same concept as SSH key fingerprints

	  This is similar to how Bitcoin derives addresses from public keys,
	  and how IPFS derives peer IDs from public keys.
	*/
	hash := sha256.Sum256(pub) // Sum256 returns [32]byte (array, not slice)
	nodeID := hex.EncodeToString(hash[:]) // [:] converts array to slice

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