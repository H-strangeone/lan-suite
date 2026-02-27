// Package identity handles JWT creation and verification for the LAN Suite API.
// When a peer "logs in" to this node's HTTP API, we issue a JWT.
// All subsequent API calls must include that JWT in the Authorization header.
package identity

/*
  CONCEPT: JWT (JSON Web Token)
  ──────────────────────────────
  A JWT is a signed, self-contained token that proves identity WITHOUT
  a database lookup on every request.

  Structure: Header.Payload.Signature (three base64 strings joined by dots)

  Header: {"alg":"HS256","typ":"JWT"}
  Payload: {"sub":"node123","exp":1735000000,"iat":1734913600,...}
  Signature: HMAC-SHA256(base64(header) + "." + base64(payload), secret)

  FLOW:
  1. Node calls POST /api/auth → gets back a JWT
  2. Node stores JWT in memory (NEVER in localStorage — XSS risk)
  3. Every API call includes: Authorization: Bearer <jwt>
  4. Server verifies signature with its secret key
  5. If valid: extract claims from payload — no database needed

  WHY IS THIS SAFE?
  The signature covers the payload. If anyone tampers with the payload
  (changes nodeId, fakes isAdmin), the signature becomes invalid.
  Only the server knows the secret key, so only the server can sign tokens.

  WHAT JWT IS NOT:
  JWT is not encryption. The payload is base64-encoded, not encrypted.
  Anyone can decode it and read the claims. Don't put secrets in JWTs.
  JWT proves identity. It doesn't hide data.

  CONCEPT: Stateless vs Stateful auth
  ─────────────────────────────────────
  Session-based (stateful): server stores session in DB/Redis.
    Pros: can invalidate instantly (logout = delete session)
    Cons: every request hits the DB, hard to scale across servers

  JWT-based (stateless): no server storage, token is self-contained.
    Pros: no DB lookup, works across multiple servers
    Cons: can't invalidate before expiry (use short expiry + refresh tokens)

  For our LAN node: JWT is perfect. We have one server, tokens are short-lived,
  and we don't need instant revocation.
*/

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is our custom JWT payload.
// It embeds jwt.RegisteredClaims which provides standard fields:
// - Subject (Sub): who the token represents
// - ExpiresAt (Exp): when the token expires
// - IssuedAt (Iat): when the token was created
// - Issuer (Iss): who issued the token
type Claims struct {
	// NodeID is this node's identity hash (from crypto/identity.go)
	NodeID string `json:"node_id"`

	// DisplayName is the human-readable name shown to peers
	DisplayName string `json:"display_name"`

	// Services lists what this node offers: ["chat","video","drive"]
	Services []string `json:"services"`

	jwt.RegisteredClaims
	/*
	  CONCEPT: Struct embedding
	  ──────────────────────────
	  Embedding jwt.RegisteredClaims means Claims "inherits" all its fields.
	  You access them directly: claims.ExpiresAt (not claims.RegisteredClaims.ExpiresAt)
	  The jwt library expects RegisteredClaims to be embedded to work correctly.
	*/
}

// Manager handles JWT operations. Holds the secret key.
// Created once at startup, passed to middleware.
type Manager struct {
	secret    []byte
	expiryHrs int
	issuer    string
}

// NewManager creates a JWT manager with the given secret and expiry.
// secret: the signing key — must be at least 32 bytes in production
// expiryHrs: how many hours the token is valid
func NewManager(secret string, expiryHrs int, nodeID string) *Manager {
	return &Manager{
		secret:    []byte(secret),
		expiryHrs: expiryHrs,
		issuer:    "lan-suite:" + nodeID,
	}
}

// Issue creates and signs a new JWT for the given node identity.
// Returns the signed token string to send to the client.
func (m *Manager) Issue(nodeID, displayName string, services []string) (string, error) {
	now := time.Now()
	expiry := now.Add(time.Duration(m.expiryHrs) * time.Hour)

	claims := &Claims{
		NodeID:      nodeID,
		DisplayName: displayName,
		Services:    services,
		RegisteredClaims: jwt.RegisteredClaims{
			// Sub: the subject — "who is this token for?"
			Subject: nodeID,
			// Iss: the issuer — "who created this token?"
			Issuer: m.issuer,
			// IssuedAt: for auditing and refresh token logic
			IssuedAt: jwt.NewNumericDate(now),
			// ExpiresAt: token is invalid after this timestamp
			ExpiresAt: jwt.NewNumericDate(expiry),
			// NotBefore: token is invalid BEFORE this timestamp (we set to now)
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	/*
	  CONCEPT: Signing algorithms
	  ─────────────────────────────
	  HS256 (HMAC-SHA256): symmetric — same key for sign + verify.
	    Use when: you control both sides (our case — single node server).
	    Risk: anyone with the key can forge tokens.

	  RS256 (RSA-SHA256): asymmetric — private key signs, public key verifies.
	    Use when: multiple servers verify but only one issues tokens.

	  ES256 (ECDSA-SHA256): asymmetric like RS256 but smaller keys.

	  For a single LAN node server: HS256 is correct and simpler.
	  Our server both issues and verifies, and the secret never leaves the server.
	*/
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return signed, nil
}

// Verify parses and validates a JWT string.
// Returns the claims if valid, error if expired/tampered/invalid.
func (m *Manager) Verify(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		/*
		  CONCEPT: Keyfunc
		  ──────────────────
		  ParseWithClaims calls this function to get the signing key.
		  It receives the token header (before verification) so you can
		  use different keys for different tokens if needed.
		  We always return the same key.

		  CRITICAL: always check method.(*jwt.SigningMethodHMAC) — this
		  ensures the token was signed with HMAC. Without this check,
		  an attacker could send a token with alg:"none" (no signature)
		  and the library would accept it!
		  This is the "algorithm confusion" attack and it's real.
		*/
		func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.secret, nil
		},
	)

	if err != nil {
		/*
		  Translate jwt errors to user-friendly messages.
		  Don't expose internal error details to clients.
		*/
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, fmt.Errorf("token expired")
		case errors.Is(err, jwt.ErrTokenNotValidYet):
			return nil, fmt.Errorf("token not yet valid")
		case errors.Is(err, jwt.ErrTokenMalformed):
			return nil, fmt.Errorf("malformed token")
		default:
			return nil, fmt.Errorf("invalid token")
		}
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}