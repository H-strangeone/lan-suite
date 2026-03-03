package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/H-strangeone/lan-suite/internal/chat"
)

type ChatHandler struct {
	manager *chat.Manager
}

func NewChatHandler(manager *chat.Manager) *ChatHandler {
	return &ChatHandler{manager: manager}
}
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	path := strings.TrimPrefix(r.URL.Path, "/api/chat/")
	path = strings.TrimSuffix(path, "/messages")
	roomID := strings.TrimSpace(path)

	if roomID == "" {
		http.Error(w, "room ID required", http.StatusBadRequest)
		return
	}
	afterSeqno := uint64(0)
	if s := r.URL.Query().Get("after"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			http.Error(w, "invalid 'after' parameter", http.StatusBadRequest)
			return
		}
		afterSeqno = n
	}

	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 200 {
			http.Error(w, "limit must be 1-200", http.StatusBadRequest)
			return
		}
		limit = n
	}

	msgs, err := h.manager.GetHistory(roomID, afterSeqno, limit)
	if err != nil {
		http.Error(w, "failed to load history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"room":       roomID,
		"messages":   msgs,
		"latest_seq": h.manager.LatestSeqno(roomID),
	})
}
