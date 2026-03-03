package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

type RoomMeta struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatorID    string    `json:"creator_id"`
	CreatorName  string    `json:"creator_name"`
	PasswordHash string    `json:"password_hash,omitempty"`
	IsPublic     bool      `json:"is_public"`
	CreatedAt    time.Time `json:"created_at"`
}

type RoomInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatorID   string    `json:"creator_id"`
	CreatorName string    `json:"creator_name"`
	IsPublic    bool      `json:"is_public"`
	CreatedAt   time.Time `json:"created_at"`
	MemberCount int       `json:"member_count"`
}

type RoomRegistry struct {
	mu      sync.RWMutex
	rooms   map[string]*RoomMeta
	dataDir string
}

func NewRoomRegistry(dataDir string) (*RoomRegistry, error) {
	rr := &RoomRegistry{
		rooms:   make(map[string]*RoomMeta),
		dataDir: dataDir,
	}

	defaults := []struct{ id, name string }{
		{"general", "general"},
		{"dev", "dev"},
		{"random", "random"},
	}
	for _, d := range defaults {
		rr.rooms[d.id] = &RoomMeta{
			ID:          d.id,
			Name:        d.name,
			CreatorID:   "system",
			CreatorName: "system",
			IsPublic:    true,
			CreatedAt:   time.Time{},
		}
	}

	if err := rr.load(); err != nil {
		return nil, err
	}
	return rr, nil
}

func (rr *RoomRegistry) Create(name, creatorID, creatorName, password string) (string, *RoomInfo, error) {
	id := uuid.New().String()

	meta := &RoomMeta{
		ID:          id,
		Name:        name,
		CreatorID:   creatorID,
		CreatorName: creatorName,
		IsPublic:    password == "",
		CreatedAt:   time.Now().UTC(),
	}

	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			return "", nil, fmt.Errorf("hashing room password: %w", err)
		}
		meta.PasswordHash = string(hash)
	}

	rr.mu.Lock()
	rr.rooms[id] = meta
	rr.mu.Unlock()

	go rr.save()

	return id, meta.toInfo(0), nil
}

func (rr *RoomRegistry) Delete(roomID string) bool {
	rr.mu.Lock()
	meta, ok := rr.rooms[roomID]
	if !ok || meta.CreatorID == "system" {
		rr.mu.Unlock()
		return false
	}
	delete(rr.rooms, roomID)
	rr.mu.Unlock()
	go rr.save()
	return true
}

func (rr *RoomRegistry) CheckPassword(roomID, password string) error {
	rr.mu.RLock()
	meta, ok := rr.rooms[roomID]
	rr.mu.RUnlock()

	if !ok {
		return fmt.Errorf("room not found")
	}
	if meta.IsPublic {
		return nil
	}
	if password == "" {
		return fmt.Errorf("this room requires a password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(meta.PasswordHash), []byte(password)); err != nil {
		return fmt.Errorf("incorrect password")
	}
	return nil
}

func (rr *RoomRegistry) Get(roomID string) (*RoomInfo, bool) {
	rr.mu.RLock()
	meta, ok := rr.rooms[roomID]
	rr.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return meta.toInfo(0), true
}

func (rr *RoomRegistry) List(memberCounts map[string]int) []RoomInfo {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	out := make([]RoomInfo, 0, len(rr.rooms))
	for _, meta := range rr.rooms {
		count := memberCounts[meta.ID]
		out = append(out, *meta.toInfo(count))
	}
	return out
}

func (rr *RoomRegistry) Exists(roomID string) bool {
	rr.mu.RLock()
	_, ok := rr.rooms[roomID]
	rr.mu.RUnlock()
	return ok
}

func (rr *RoomRegistry) RoomListJSON(memberCounts map[string]int) ([]byte, error) {
	rooms := rr.List(memberCounts)
	return json.Marshal(rooms)
}

func (rr *RoomRegistry) save() {
	rr.mu.RLock()

	toSave := make(map[string]*RoomMeta)
	for id, meta := range rr.rooms {
		if meta.CreatorID != "system" {
			toSave[id] = meta
		}
	}
	rr.mu.RUnlock()

	path := filepath.Join(rr.dataDir, "rooms.json")
	data, err := json.MarshalIndent(toSave, "", "  ")
	if err != nil {
		fmt.Printf("[rooms] marshal error: %v\n", err)
		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("[rooms] write error: %v\n", err)
		return
	}
	os.Rename(tmp, path)
}

func (rr *RoomRegistry) load() error {
	path := filepath.Join(rr.dataDir, "rooms.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading rooms.json: %w", err)
	}

	var loaded map[string]*RoomMeta
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parsing rooms.json: %w", err)
	}

	rr.mu.Lock()
	for id, meta := range loaded {
		rr.rooms[id] = meta
	}
	rr.mu.Unlock()
	return nil
}

func (m *RoomMeta) toInfo(memberCount int) *RoomInfo {
	return &RoomInfo{
		ID:          m.ID,
		Name:        m.Name,
		CreatorID:   m.CreatorID,
		CreatorName: m.CreatorName,
		IsPublic:    m.IsPublic,
		CreatedAt:   m.CreatedAt,
		MemberCount: memberCount,
	}
}
