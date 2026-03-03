package chat

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/H-strangeone/lan-suite/internal/storage"
	"github.com/H-strangeone/lan-suite/internal/transport"
)

const (
	maxMessageLen = 4000
	maxRoomIDLen  = 64
)

type Message struct {
	SeqNo      uint64 `json:"seq_no"`
	ID         string `json:"id"`
	Room       string `json:"room"`
	FromNodeID string `json:"from_node_id"`
	FromName   string `json:"from_name"`
	Text       string `json:"text"`
	Timestamp  string `json:"timestamp"`
	ReplyTo    string `json:"reply_to,omitempty"`
}

type IncomingChatMsg struct {
	Text    string `json:"text"`
	ReplyTo string `json:"reply_to,omitempty"`
}

type Manager struct {
	store    *storage.Store
	hub      *transport.Hub
	Registry *RoomRegistry

	mu    sync.RWMutex
	rooms map[string]*room
}

type room struct {
	id      string
	members map[string]string
}

func New(store *storage.Store, hub *transport.Hub, dataDir string) (*Manager, error) {
	if err := store.LoadRoomSeqnos(); err != nil {
		return nil, fmt.Errorf("loading seqnos: %w", err)
	}

	reg, err := NewRoomRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("loading room registry: %w", err)
	}

	return &Manager{
		store:    store,
		hub:      hub,
		Registry: reg,
		rooms:    make(map[string]*room),
	}, nil
}

func (m *Manager) HandleMessage(nodeID, displayName, roomID string, payload []byte) error {

	roomID = strings.TrimSpace(roomID)
	if roomID == "" || len(roomID) > maxRoomIDLen || !isValidRoomID(roomID) {
		return fmt.Errorf("invalid room ID")
	}

	var incoming IncomingChatMsg
	if err := json.Unmarshal(payload, &incoming); err != nil {
		return fmt.Errorf("invalid message payload")
	}

	incoming.Text = strings.TrimSpace(incoming.Text)
	if incoming.Text == "" {
		return fmt.Errorf("message text cannot be empty")
	}
	if utf8.RuneCountInString(incoming.Text) > maxMessageLen {
		return fmt.Errorf("message too long (max %d characters)", maxMessageLen)
	}

	incoming.Text = sanitizeText(incoming.Text)

	msg := &Message{
		ID:         uuid.New().String(),
		Room:       roomID,
		FromNodeID: nodeID,
		FromName:   displayName,
		Text:       incoming.Text,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ReplyTo:    incoming.ReplyTo,
	}

	stored := &storage.StoredMessage{
		ID:         msg.ID,
		Room:       msg.Room,
		FromNodeID: msg.FromNodeID,
		FromName:   msg.FromName,
		Text:       msg.Text,
		Timestamp:  msg.Timestamp,
		ReplyTo:    msg.ReplyTo,
	}

	seqno, err := m.store.SaveMessage(stored)
	if err != nil {
		return fmt.Errorf("saving message: %w", err)
	}
	msg.SeqNo = seqno

	log.Printf("[chat] room=%s seq=%d from=%s: %s",
		roomID, seqno, displayName, truncate(msg.Text, 50))

	payload, err = json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling broadcast: %w", err)
	}

	m.hub.BroadcastToRoom(roomID, &transport.Message{
		Type:    transport.TypeChatMsg,
		Room:    roomID,
		From:    nodeID,
		Payload: payload,
	})

	return nil
}

func (m *Manager) CreateRoom(name, creatorID, creatorName, password string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", fmt.Errorf("room name must be 1-64 characters")
	}
	if !isValidRoomID(name) {
		return "", fmt.Errorf("room name may only contain a-z, 0-9, hyphens, underscores")
	}
	id, _, err := m.Registry.Create(name, creatorID, creatorName, password)
	return id, err
}

func (m *Manager) RoomListJSON(memberCounts map[string]int) ([]byte, error) {
	return m.Registry.RoomListJSON(memberCounts)
}

func (m *Manager) CheckRoomPassword(roomID, password string) error {
	return m.Registry.CheckPassword(roomID, password)
}

func (m *Manager) DeleteRoom(roomID string) {
	m.Registry.Delete(roomID)
}

func (m *Manager) RoomExists(roomID string) bool {
	return m.Registry.Exists(roomID)
}

func (m *Manager) GetHistory(roomID string, afterSeqno uint64, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 50
	}

	stored, err := m.store.LoadMessages(roomID, afterSeqno, limit)
	if err != nil {
		return nil, err
	}

	msgs := make([]*Message, 0, len(stored))
	for _, s := range stored {
		msgs = append(msgs, &Message{
			SeqNo:      s.SeqNo,
			ID:         s.ID,
			Room:       s.Room,
			FromNodeID: s.FromNodeID,
			FromName:   s.FromName,
			Text:       s.Text,
			Timestamp:  s.Timestamp,
			ReplyTo:    s.ReplyTo,
		})
	}
	return msgs, nil
}

func (m *Manager) LatestSeqno(roomID string) uint64 {
	return m.store.LatestSeqno(roomID)
}

func sanitizeText(s string) string {
	return strings.Map(func(r rune) rune {

		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		if r >= 0x7F && r <= 0x9F {
			return -1
		}
		return r
	}, s)
}

func isValidRoomID(id string) bool {
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
