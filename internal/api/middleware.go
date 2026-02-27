// Package api contains the HTTP/WebSocket API server and all its middleware.
package api

/*
  CONCEPT: Middleware (the most important pattern in web backends)
  ─────────────────────────────────────────────────────────────────
  A middleware is a function that wraps an HTTP handler.
  It runs BEFORE (and optionally after) the handler.

  The shape of every middleware is always the same:
    func SomeMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // --- BEFORE handler ---
            // validate, authenticate, log, rate limit...

            next.ServeHTTP(w, r)  // call the actual handler

            // --- AFTER handler ---
            // log response time, set headers, cleanup...
        })
    }

  Middleware chains:
  Request → [Logger] → [CORS] → [RateLimit] → [Auth] → [Handler]
  Response ←          ←        ←            ←         ←

  Each middleware decides whether to:
  - Call next.ServeHTTP(w, r) → pass to next middleware/handler
  - Call http.Error(w, ...) and RETURN → short-circuit the chain

  WHY MIDDLEWARE OVER INLINE CHECKS?
  If you have 20 protected routes, you don't write auth checks in all 20.
  You write one auth middleware and wrap all 20 routes with it.
  One place to audit. One place to change. Zero duplication.

  CONCEPT: http.Handler interface
  ─────────────────────────────────
  http.Handler is just an interface with one method:
    type Handler interface {
        ServeHTTP(ResponseWriter, *Request)
    }
  http.HandlerFunc is a function type that implements this interface.
  So any function with the right signature IS an http.Handler.
  This is how Go does polymorphism — small, composable interfaces.
*/

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/H-strangeone/lan-suite/internal/config"
	"github.com/H-strangeone/lan-suite/internal/identity"
)

// contextKey is an unexported type for context keys.
// This prevents collisions with keys from other packages.
type contextKey string

const (
	// ClaimsKey is the context key for JWT claims.
	// After Auth middleware runs, handlers retrieve claims with:
	//   claims := r.Context().Value(ClaimsKey).(*identity.Claims)
	ClaimsKey contextKey = "claims"
)

// ─── CORS Middleware ──────────────────────────────────────────────────────────

/*
  CONCEPT: CORS (Cross-Origin Resource Sharing)
  ───────────────────────────────────────────────
  The browser's "same-origin policy" blocks JavaScript from making requests
  to a different origin (protocol + host + port) than the page's origin.

  Example: your React app at http://localhost:5173 wants to call
  http://localhost:8080/api. Different ports = different origins = BLOCKED.

  CORS is the mechanism that lets a server say "it's okay, I allow this origin".
  The server adds response headers:
    Access-Control-Allow-Origin: http://localhost:5173
    Access-Control-Allow-Methods: GET, POST, OPTIONS
    Access-Control-Allow-Headers: Content-Type, Authorization

  PREFLIGHT: Before a complex request (POST with JSON body), browsers send
  an OPTIONS request first to check if CORS is allowed.
  Your server must respond to OPTIONS requests correctly.

  SECURITY: Never use Access-Control-Allow-Origin: *  with credentials.
  Always use an explicit allowlist of trusted origins.
  In our case: only our own frontend origins are trusted.

  CONCEPT: Why you shouldn't rely on client-side CORS checks
  ─────────────────────────────────────────────────────────────
  CORS is enforced by the BROWSER, not the server itself.
  curl, Postman, Python scripts, mobile apps — they ignore CORS entirely.
  CORS protects users from malicious websites making requests on their behalf.
  It's not an API security mechanism. Auth headers are.
*/

// CORS returns middleware that sets CORS headers for allowed origins.
func CORS(cfg *config.Config) func(http.Handler) http.Handler {
	// Build a set for O(1) origin lookup
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[strings.ToLower(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				if allowed[strings.ToLower(origin)] || cfg.IsDev() {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Max-Age", "86400") // cache preflight for 24h
				}
			}

			// Handle preflight — OPTIONS must return 204 with no body
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ─── Security Headers Middleware ──────────────────────────────────────────────

/*
  CONCEPT: Security Headers
  ──────────────────────────
  These HTTP response headers tell browsers to enforce additional protections.

  Content-Security-Policy: restricts what resources the page can load.
    Prevents XSS by blocking inline scripts and external script sources.

  X-Content-Type-Options: nosniff
    Prevents browser "MIME sniffing" — guessing content type from file content.
    Without this, a malicious file named image.png with HTML inside could
    be executed as HTML by the browser.

  X-Frame-Options: DENY
    Prevents your site from being embedded in an iframe.
    Blocks "clickjacking" — attacker embeds your site invisibly, tricks
    user into clicking buttons on your site while they think they're clicking
    on the attacker's site.

  Referrer-Policy: strict-origin-when-cross-origin
    Controls how much URL information is sent in the Referer header.
    Prevents leaking internal URLs to external sites.

  CONCEPT: HSTS (HTTP Strict Transport Security)
  ─────────────────────────────────────────────────
  Strict-Transport-Security: max-age=31536000
    Tells the browser: "for the next year, ONLY connect to me over HTTPS.
    Even if the user types http://, upgrade to https:// automatically."

    After the first HTTPS response, the browser remembers this.
    Attacker can't downgrade the connection to HTTP to intercept traffic.

    includeSubDomains: applies to all subdomains too.
    preload: submit to browser preload lists (built-in HSTS, no first request needed).

  We only send HSTS in production (HTTPS). In dev we use HTTP.
*/

// SecureHeaders adds security-oriented HTTP response headers.
func SecureHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Only send HSTS in production (requires HTTPS to be meaningful)
			if !cfg.IsDev() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ─── Rate Limiting Middleware ─────────────────────────────────────────────────

/*
  CONCEPT: Rate Limiting
  ───────────────────────
  Without rate limiting, a single client can:
  - Brute-force passwords (try millions of combinations)
  - DoS your server with millions of requests
  - Scrape all your data instantly

  Token Bucket algorithm (what we implement here):
  - Each IP gets a "bucket" that holds N tokens
  - Each request costs 1 token
  - The bucket refills at R tokens/minute
  - If bucket is empty → 429 Too Many Requests

  Visual:
  Bucket [●●●●●●●●●●] 10 tokens
  Request arrives → [●●●●●●●●●] 9 tokens
  Request arrives → [●●●●●●●●] 8 tokens
  ... 
  Bucket empty → 429

  After 1 minute: bucket refills to 10 tokens again.

  For login endpoints: use stricter limits (5 attempts/minute).
  For general API: 120/minute is reasonable.
  For WebSocket upgrade: 10 connections per IP.
*/

// rateLimitEntry tracks a single IP's request count.
type rateLimitEntry struct {
	count     int
	resetAt   time.Time
	mu        sync.Mutex
}

// RateLimiter holds state for all IPs.
type RateLimiter struct {
	entries    map[string]*rateLimitEntry
	mu         sync.RWMutex
	maxPerMin  int
	cleanupInt time.Duration
}

// NewRateLimiter creates a rate limiter with the given per-minute limit.
func NewRateLimiter(maxPerMin int) *RateLimiter {
	rl := &RateLimiter{
		entries:    make(map[string]*rateLimitEntry),
		maxPerMin:  maxPerMin,
		cleanupInt: 5 * time.Minute,
	}
	// Background goroutine cleans up expired entries to prevent memory leak
	go rl.cleanup()
	return rl
}

// Allow checks if the IP is within rate limits.
// Returns true if request is allowed, false if limited.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	entry, exists := rl.entries[ip]
	if !exists {
		entry = &rateLimitEntry{}
		rl.entries[ip] = entry
	}
	rl.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now()
	// Reset counter if the minute window has passed
	if now.After(entry.resetAt) {
		entry.count = 0
		entry.resetAt = now.Add(time.Minute)
	}

	if entry.count >= rl.maxPerMin {
		return false
	}
	entry.count++
	return true
}

// Middleware returns an http.Handler middleware that enforces rate limits.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)
		if !rl.Allow(ip) {
			/*
			  429 Too Many Requests is the correct status code.
			  Retry-After tells clients when they can try again.
			  Don't return 403 (Forbidden) — that means permanently blocked.
			*/
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) cleanup() {
	for range time.Tick(rl.cleanupInt) {
		rl.mu.Lock()
		now := time.Now()
		for ip, entry := range rl.entries {
			entry.mu.Lock()
			if now.After(entry.resetAt) {
				delete(rl.entries, ip)
			}
			entry.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// ─── Auth Middleware ──────────────────────────────────────────────────────────

/*
  CONCEPT: Authentication vs Authorization
  ──────────────────────────────────────────
  Authentication: "Who are you?" — verify identity (JWT, password)
  Authorization:  "What can you do?" — check permissions (role, ownership)

  Auth middleware handles Authentication.
  It runs on every protected route and:
  1. Reads the Authorization: Bearer <token> header
  2. Verifies the JWT signature and expiry
  3. Extracts claims (nodeID, displayName, etc.)
  4. Puts claims in the request context for handlers to use
  5. If invalid/missing → 401 Unauthorized, stop here

  CONCEPT: Why check auth on EVERY protected route?
  ───────────────────────────────────────────────────
  Because HTTP is stateless. Every request is independent.
  There is no "logged in state" between requests.
  Each request must prove its own identity via the token.

  CONCEPT: Why not rely on client-side checks?
  ──────────────────────────────────────────────
  The client (browser/app) can be modified by the user.
  - In React: if (user.isAdmin) showAdminButton() → user edits JS → they see button
  - Without server-side check: they click it → server executes admin action
  With server-side auth: even if they bypass the UI, the server rejects the request.
  The server is the ONLY source of truth. Always.

  CONCEPT: Bearer tokens
  ───────────────────────
  The Authorization header format:
    Authorization: Bearer eyJhbGci...
  "Bearer" means "the bearer of this token is authorized".
  The token is self-contained proof of identity.
  Extract: strings.TrimPrefix(header, "Bearer ")
*/

// Auth returns middleware that requires a valid JWT.
// Protected routes must be wrapped with this.
// Public routes (like /api/auth, /health) must NOT be wrapped.
func Auth(jwtManager *identity.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization: Bearer <token>
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, "invalid authorization format", http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(header, "Bearer ")
			if tokenStr == "" {
				http.Error(w, "empty token", http.StatusUnauthorized)
				return
			}

			// Verify the JWT — checks signature, expiry, and all registered claims
			claims, err := jwtManager.Verify(tokenStr)
			if err != nil {
				// Note: we don't log the specific error to the client.
				// "Invalid token" is enough — no info leaked to attackers.
				log.Printf("[auth] rejected token from %s: %v", realIP(r), err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			/*
			  CONCEPT: context.WithValue
			  ───────────────────────────
			  The request context is a bag of key-value pairs that flows
			  through the middleware chain into the handler.
			  We put the verified claims in context so handlers can access them
			  without re-parsing the JWT.

			  Handler access pattern:
			    claims, ok := r.Context().Value(ClaimsKey).(*identity.Claims)
			    if !ok { ... } // type assertion failed
			*/
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ─── Logger Middleware ────────────────────────────────────────────────────────

// responseWriter wraps http.ResponseWriter to capture the status code.
// The standard ResponseWriter doesn't expose the status code after WriteHeader.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.size += len(b)
	return rw.ResponseWriter.Write(b)
}

// Logger logs each HTTP request: method, path, status, duration, IP.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		log.Printf("[http] %s %s %d %s %s",
			r.Method,
			r.URL.Path,
			rw.status,
			duration.Round(time.Microsecond),
			realIP(r),
		)
	})
}

// ─── Recovery Middleware ──────────────────────────────────────────────────────

/*
  CONCEPT: Panic recovery
  ────────────────────────
  In Go, a "panic" is an unrecoverable error (nil pointer dereference,
  index out of bounds, etc.). By default, a panic in an HTTP handler
  kills the entire server process.

  recover() catches panics within a deferred function.
  We use this to catch unexpected panics, log them, and return 500
  instead of crashing the whole server.

  This is the Go equivalent of a try/catch(Exception e) in Java.
*/

// Recovery catches panics in handlers and returns 500 instead of crashing.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[recovery] panic: %v — path: %s", err, r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// realIP extracts the client's real IP from the request.
// Handles reverse proxy headers (X-Forwarded-For, X-Real-IP).
func realIP(r *http.Request) string {
	/*
	  CONCEPT: X-Forwarded-For
	  ──────────────────────────
	  When behind a reverse proxy (Nginx, Caddy, load balancer),
	  the TCP connection comes from the proxy, not the client.
	  r.RemoteAddr would be the proxy's IP (useless for rate limiting).

	  Proxies add X-Forwarded-For: <client-ip>, <proxy1-ip>, <proxy2-ip>
	  The first IP in the list is the original client.

	  WARNING: X-Forwarded-For can be spoofed by clients directly.
	  Only trust it when you KNOW you're behind a trusted proxy.
	  For our LAN suite: we're not behind a proxy in dev, so we fall through
	  to RemoteAddr which is always accurate in direct connections.
	*/
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// RemoteAddr is "ip:port" — strip the port
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Chain applies a list of middleware functions to a handler, in order.
// Usage: handler = Chain(myHandler, Logger, CORS(cfg), Auth(jwt))
// Request flows: Logger → CORS → Auth → myHandler
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	// Apply in reverse so the first middleware in the list runs first
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// GetClaims extracts JWT claims from the request context.
// Returns nil if no claims (unauthenticated route).
// Handler usage: claims := api.GetClaims(r)
func GetClaims(r *http.Request) *identity.Claims {
	claims, _ := r.Context().Value(ClaimsKey).(*identity.Claims)
	return claims
}

// respond writes a JSON response. We'll expand this in the handlers file.
func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}