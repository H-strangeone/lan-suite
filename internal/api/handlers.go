package api

/*
  CONCEPT: Input validation — "Never Trust The User"
  ────────────────────────────────────────────────────
  Every piece of data from a client can be:
  - Missing entirely
  - The wrong type (string where you expect int)
  - Too long (10MB JSON body when you expect 100 bytes)
  - Malicious (SQL injection, XSS payloads, null bytes)
  - Structurally valid but semantically wrong ("age": -500)

  LAYERS OF VALIDATION:
  1. Size limits — reject before even parsing (prevent memory exhaustion)
  2. Type checking — JSON decoder catches wrong types
  3. Required fields — check for empty/missing values
  4. Format validation — regex, length limits, character allowlists
  5. Business logic — "this room ID must exist" etc.

  Every layer stops a different class of attack.
  Never skip any layer because "I trust this client".
  The client is not your code. The client is attacker-controlled.

  CONCEPT: Parameterized queries (for when we add a database)
  ────────────────────────────────────────────────────────────
  We don't use a SQL database yet, but when we do:

  WRONG (SQL injection vulnerable):
    query := "SELECT * FROM users WHERE name = '" + name + "'"
    // if name = "'; DROP TABLE users; --"
    // → SELECT * FROM users WHERE name = ''; DROP TABLE users; --'
    // Your database is gone.

  RIGHT (parameterized):
    row := db.QueryRow("SELECT * FROM users WHERE name = ?", name)
    // The ? placeholder is NEVER executed as SQL.
    // The database driver sends query and data SEPARATELY.
    // SQL injection is structurally impossible.

  ORMs (like GORM) use parameterized queries internally.
  But even with an ORM, raw queries can be vulnerable if you
  interpolate strings into them. Always use the ORM's query builder.

  CONCEPT: CSRF tokens
  ─────────────────────
  Cross-Site Request Forgery: an attacker's site tricks your browser
  into making a request to our API using your stored cookies.

  Defense 1: SameSite=Strict cookies — browser won't send cookies
             for cross-site requests. Kills most CSRF attacks.
  Defense 2: CSRF token — a secret value in each form that the
             server verifies. An attacker can't read our pages,
             so they can't get the token.

  For our API: we use JWTs in Authorization headers (not cookies).
  Authorization headers can ONLY be set by JavaScript on the same origin.
  So CSRF is not a concern for header-based auth. No CSRF tokens needed.

  But: if you ever use cookies for auth, add CSRF tokens immediately.

  CONCEPT: Verify Origin headers
  ───────────────────────────────
  For WebSocket connections, the browser sends an Origin header.
  We check it against our allowed origins list.
  This is SEPARATE from CORS — it's a server-side check.
  CORS is enforced by browsers. Origin check is enforced by our code.
  A non-browser client (curl) might omit the Origin header.
  For WebSocket upgrades on a public API, you must check this.
*/

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/H-strangeone/lan-suite/internal/config"
	"github.com/H-strangeone/lan-suite/internal/crypto"
	"github.com/H-strangeone/lan-suite/internal/identity"
)

// AuthHandler handles POST /api/auth — issues JWT tokens.
// This is a "constructor" — NewAuthHandler returns the handler.
// We use a struct so the handler can hold references to its dependencies.
type AuthHandler struct {
	jwt      *identity.Manager
	cfg      *config.Config
	identity *crypto.Identity
}

// NewAuthHandler creates an AuthHandler.
// Dependencies are injected — this makes testing easy.
func NewAuthHandler(jwt *identity.Manager, cfg *config.Config, id *crypto.Identity) http.Handler {
	h := &AuthHandler{jwt: jwt, cfg: cfg, identity: id}
	return h
}

// ServeHTTP implements http.Handler.
// This is what runs when POST /api/auth is called.
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ── Layer 1: Request method check ────────────────────────────────────────
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ── Layer 2: Body size limit ──────────────────────────────────────────────
	/*
	  Without this, a client can send a 1GB body and exhaust your RAM.
	  http.MaxBytesReader wraps the body with a reader that returns an error
	  after reading more than maxBytes. 4KB is very generous for a login request.
	  If they need more than 4KB, they're doing something wrong.
	*/
	const maxBodyBytes = 4 * 1024 // 4 KB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	// ── Layer 3: Parse JSON body ───────────────────────────────────────────────
	var req struct {
		// DisplayName is the human-readable name for this peer
		DisplayName string `json:"display_name"`
		// Services lists what this node offers
		Services []string `json:"services"`
		// Secret is the node access passphrase (optional — only if configured)
		Secret string `json:"secret,omitempty"`
	}

	decoder := json.NewDecoder(r.Body)
	/*
	  DisallowUnknownFields makes the decoder return an error if the
	  request contains fields we didn't declare.
	  Without this: extra fields are silently ignored (usually fine).
	  With this: we're strict about what we accept.
	  Use it when you want to catch client-side bugs early.
	*/
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			// Note: return a generic error, not the actual parse error.
			// Parse errors from json.Decoder can leak internal struct info.
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
		}
		return
	}

	// ── Layer 4: Field validation ─────────────────────────────────────────────
	/*
	  CONCEPT: Allowlist vs Denylist validation
	  ──────────────────────────────────────────
	  Denylist: "reject if contains <script> or ; or '"
	  Problem: attacker uses unicode lookalikes, encoding tricks, etc.
	  You can never list all bad things.

	  Allowlist: "only accept characters in [a-zA-Z0-9 -_]"
	  Problem: more restrictive, but structurally impossible to bypass.
	  Always prefer allowlists where possible.

	  Here: we validate length and UTF-8 validity, then strip control chars.
	  We don't restrict to ASCII — display names can be Unicode (emojis, CJK).
	  But we do strip null bytes and control characters which can break rendering.
	*/

	// Validate DisplayName
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		http.Error(w, "display_name is required", http.StatusBadRequest)
		return
	}
	if !utf8.ValidString(req.DisplayName) {
		http.Error(w, "display_name must be valid UTF-8", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(req.DisplayName) > 64 {
		http.Error(w, "display_name must be 64 characters or fewer", http.StatusBadRequest)
		return
	}
	// Strip control characters (null bytes, newlines, etc.)
	req.DisplayName = sanitizeString(req.DisplayName)

	// Validate Services
	if len(req.Services) == 0 {
		req.Services = []string{"chat"} // default
	}
	if len(req.Services) > 10 {
		http.Error(w, "too many services", http.StatusBadRequest)
		return
	}
	validServices := map[string]bool{"chat": true, "video": true, "drive": true, "mail": true}
	for _, svc := range req.Services {
		if !validServices[svc] {
			http.Error(w, fmt.Sprintf("unknown service: %q", svc), http.StatusBadRequest)
			return
		}
	}

	// ── Layer 5: Secret verification (if node requires one) ───────────────────
	/*
	  If the node has a configured access secret (hashed bcrypt string in config),
	  verify the provided secret against it.
	  If no secret configured: open access (suitable for trusted LAN).
	  Never store the plaintext secret — only the bcrypt hash.
	*/
	if h.cfg.JWTSecret != "" && !h.cfg.IsDev() {
		// In a real deployment, you'd store a hashed access secret separately from JWTSecret.
		// For now: any client can auth. Node-level access control is Phase 2+.
		// TODO: add ACCESS_SECRET config for node access control
	}

	// ── Issue JWT ─────────────────────────────────────────────────────────────
	/*
	  The nodeID here is not the permanent node identity — it's a session ID.
	  We use a combination of the claimed display name and a random UUID.
	  The permanent identity (Ed25519 keypair) is used separately for
	  peer-to-peer signing — not for API auth.
	  API auth just needs: "is this a legitimate client of this node?".
	*/
	sessionNodeID := fmt.Sprintf("%s-%s", sanitizeString(req.DisplayName), shortID())

	tokenStr, err := h.jwt.Issue(sessionNodeID, req.DisplayName, req.Services)
	if err != nil {
		log.Printf("[auth] failed to issue JWT: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("[auth] issued token for %q services=%v", req.DisplayName, req.Services)

	// ── Respond ───────────────────────────────────────────────────────────────
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":        tokenStr,
		"node_id":      sessionNodeID,
		"display_name": req.DisplayName,
		"services":     req.Services,
		// Tell client when the token expires so it can refresh
		"expires_in_hrs": h.cfg.JWTExpiryHrs,
	})
}

// sanitizeString removes control characters from a string.
// Keeps printable Unicode, removes null bytes, escape sequences, etc.
func sanitizeString(s string) string {
	return strings.Map(func(r rune) rune {
		// Remove control characters (U+0000 to U+001F and U+007F to U+009F)
		// These can break terminals, JSON parsers, and rendering.
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return -1 // -1 means "drop this character"
		}
		return r
	}, s)
}

// shortID generates a short random hex string for session IDs.
// Not a full UUID — just 8 hex chars for readability.
func shortID() string {
	b := make([]byte, 4)
	// crypto/rand is imported via the crypto package we built
	// For this file we use the simpler approach:
	io.ReadFull(cryptoRandReader, b)
	return fmt.Sprintf("%x", b)
}

// cryptoRandReader alias — avoids importing crypto/rand in multiple files.
// Set in a separate init file to avoid circular imports.
var cryptoRandReader = cryptoRandReaderImpl{}

type cryptoRandReaderImpl struct{}

func (cryptoRandReaderImpl) Read(b []byte) (int, error) {
	// Use crypto/rand.Read directly
	return readCryptoRand(b)
}