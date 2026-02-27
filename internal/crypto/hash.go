package crypto

/*
  CONCEPT: Why hash passwords at all?
  ─────────────────────────────────────
  If you store passwords as plaintext and your database is leaked,
  every user's password is exposed — and since people reuse passwords,
  this also compromises their other accounts.

  A hash is a one-way function: easy to compute, impossible to reverse.
  SHA256("password") → "5e884..." always.
  "5e884..." → ??? (you can't reverse this)

  But: attackers use precomputed "rainbow tables" — massive lookup tables
  of hash(common_password). SHA256 alone is too fast and vulnerable to this.

  CONCEPT: bcrypt
  ────────────────
  bcrypt solves rainbow tables with two features:
  1. SALT: a random value added to each password before hashing.
     Same password + different salt = completely different hash.
     Rainbow tables become useless because they'd need to be rebuilt
     for every possible salt.

  2. COST FACTOR (work factor): bcrypt is intentionally SLOW.
     Cost=10 means 2^10 = 1024 iterations.
     Hashing one password takes ~100ms on modern hardware.
     For a legitimate login: unnoticeable.
     For an attacker trying 1 billion passwords: 100,000 years.

  bcrypt output includes the salt and cost factor in the hash itself:
  $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
  ^^^^  ^^  ^^^^^^^^^^^^^^^^^^^^^^^^^^^
  algo  cost  salt(22 chars)  hash(31 chars)

  So you only ever store ONE field: the bcrypt output.
  Verification: bcrypt.CompareHashAndPassword(stored, input)
  bcrypt internally extracts the salt from the stored hash.

  IN THIS PROJECT: We use bcrypt for the node "access secret" —
  a password an admin sets so only authorized nodes can join.
  Not every LAN app needs auth, but when you want it, this is how.
*/

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost is the work factor for bcrypt.
	// 12 is a good balance for 2024 hardware:
	// - Fast enough: ~250ms per hash (imperceptible to users)
	// - Slow enough: brute force is computationally infeasible
	// Increase this as hardware gets faster. Never decrease it.
	BcryptCost = 12
)

// HashSecret bcrypt-hashes a secret (password/passphrase).
// The result is safe to store in a database or config file.
// Call this when setting/changing a secret — NOT on every login check.
func HashSecret(secret string) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("secret cannot be empty")
	}

	/*
	  bcrypt has a 72-byte input limit. Anything beyond 72 bytes is silently
	  truncated. For most passwords this doesn't matter, but it's worth knowing.
	  If you need longer inputs, pre-hash with SHA256 first:
	    prehashed := sha256.Sum256([]byte(secret))
	    bcrypt.GenerateFromPassword(prehashed[:], cost)
	  We don't do this here because our secrets are short node passphrases.
	*/
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing secret: %w", err)
	}

	return string(hash), nil
}

// VerifySecret checks if secret matches the stored bcrypt hash.
// Returns nil if they match, error if they don't.
// TIMING: This always takes ~250ms regardless of input, which is intentional.
// Constant-time comparison prevents timing attacks.
func VerifySecret(hash, secret string) error {
	/*
	  CONCEPT: Timing attacks
	  ────────────────────────
	  A naive string comparison: "abc" vs "xyz"
	  - Compare 'a' vs 'x' → mismatch → return false immediately

	  An attacker can measure how long the comparison takes.
	  If it returns faster, fewer characters matched.
	  This leaks information about the correct value character by character.

	  bcrypt.CompareHashAndPassword uses constant-time comparison internally.
	  It always takes the same time regardless of where the mismatch occurs.
	  This closes the timing attack vector.
	*/
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret))
	if err != nil {
		// Don't return the bcrypt error directly — it might leak info.
		// Return a generic "invalid" message.
		return fmt.Errorf("invalid secret")
	}
	return nil
}

// HashBytes returns the hex-encoded SHA256 hash of data.
// Used for:
// - Content addressing (file chunks): hash = the chunk's "name"
// - Integrity verification: re-hash received chunk, compare to expected
// - Merkle tree nodes (Phase 4)
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile computes the SHA256 hash of a file by reading it in chunks.
// More memory-efficient than reading the whole file into memory.
// Used to verify file integrity in the distributed drive.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hashing: %w", err)
	}
	defer f.Close()
	/*
	  CONCEPT: defer
	  ───────────────
	  defer runs when the function returns — regardless of how it returns
	  (normal return, panic, or any return path).
	  It's Go's way of ensuring cleanup happens: close files, release locks,
	  stop timers, etc.

	  Golden rule: always defer f.Close() immediately after os.Open().
	  If you forget and the function panics or returns early,
	  the file descriptor leaks. The OS only allows ~1024 open files per process.
	*/

	h := sha256.New()

	/*
	  io.Copy reads from f in 32KB chunks and writes to h (the hasher).
	  The hasher's Write method updates the running hash state.
	  At the end, h.Sum(nil) returns the final hash.
	  For a 1GB file: uses 32KB of RAM, not 1GB. This is streaming.
	*/
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}