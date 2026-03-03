package api

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

type contextKey string

const (
	ClaimsKey contextKey = "claims"
)

func CORS(cfg *config.Config) func(http.Handler) http.Handler {

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
					w.Header().Set("Access-Control-Max-Age", "86400")
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
func SecureHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			if !cfg.IsDev() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

type rateLimitEntry struct {
	count   int
	resetAt time.Time
	mu      sync.Mutex
}
type RateLimiter struct {
	entries    map[string]*rateLimitEntry
	mu         sync.RWMutex
	maxPerMin  int
	cleanupInt time.Duration
}

func NewRateLimiter(maxPerMin int) *RateLimiter {
	rl := &RateLimiter{
		entries:    make(map[string]*rateLimitEntry),
		maxPerMin:  maxPerMin,
		cleanupInt: 5 * time.Minute,
	}

	go rl.cleanup()
	return rl
}

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

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)
		if !rl.Allow(ip) {

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
func Auth(jwtManager *identity.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string
			if header := r.Header.Get("Authorization"); header != "" {
				if !strings.HasPrefix(header, "Bearer ") {
					http.Error(w, "invalid authorization format", http.StatusUnauthorized)
					return
				}
				tokenStr = strings.TrimPrefix(header, "Bearer ")
			} else if q := r.URL.Query().Get("token"); q != "" {
				tokenStr = q
			}

			if tokenStr == "" {
				log.Printf("[auth] no token: path=%s query=%s", r.URL.Path, r.URL.RawQuery)
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}
			claims, err := jwtManager.Verify(tokenStr)
			if err != nil {

				log.Printf("[auth] rejected token from %s: %v", realIP(r), err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

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

func realIP(r *http.Request) string {

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {

	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}
func GetClaims(r *http.Request) *identity.Claims {
	claims, _ := r.Context().Value(ClaimsKey).(*identity.Claims)
	return claims
}

func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}
