package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"crypto/rand"
	"fmt"

	"github.com/H-strangeone/lan-suite/internal/identity"
)

type authRequest struct {
	DisplayName string   `json:"display_name"`
	Services    []string `json:"services"`
}

type authResponse struct {
	Token       string   `json:"token"`
	NodeID      string   `json:"node_id"`
	DisplayName string   `json:"display_name"`
	Services    []string `json:"services"`
}

func AuthHandler(jwtMgr *identity.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req authRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		req.DisplayName = strings.TrimSpace(req.DisplayName)
		if req.DisplayName == "" {
			http.Error(w, "display_name required", http.StatusBadRequest)
			return
		}
		if len(req.Services) == 0 {
			req.Services = []string{"chat"}
		}
		sessionNodeID := fmt.Sprintf(
			"%s-%s",
			sanitize(req.DisplayName),
			shortID(),
		)

		token, err := jwtMgr.Issue(sessionNodeID, req.DisplayName, req.Services)
		if err != nil {
			http.Error(w, "failed to issue token", http.StatusInternalServerError)
			return
		}

		resp := authResponse{
			Token:       token,
			NodeID:      sessionNodeID,
			DisplayName: req.DisplayName,
			Services:    req.Services,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
func shortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
